package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestAutomationDeliveryRetainsLeaseUntilLateMattermostPostIsConfirmed(t *testing.T) {
	const runID = "scheduled-run-44444444444444444444444444444444"
	start := time.Date(2026, time.July, 22, 14, 0, 0, 0, time.UTC)
	now := start
	repository := &lateVisibilityAutomationRepository{
		run: entity.ScheduledRun{ID: 44, PublicID: runID, ProjectID: 1, RuntimeTurnID: 9},
		gateContext: entity.AutomationOwnerGateContext{
			ScheduledRunID: 44, ScheduledRunPublicID: runID, ProjectID: 1, RuntimeTurnID: 9,
			ProcessRunID: 15, ProcessPublicID: "process-run-4", PolicyRevisionID: 19,
			RootInitiatorUserID: "owner-id", RootInitiatorName: "owner",
			MattermostChannelID: "channel-1", MattermostRootPostID: "root-1",
		},
	}
	var visible atomic.Bool
	var getCalls atomic.Int64
	var postCalls atomic.Int64
	var createdMu sync.Mutex
	var created *mattermostmodel.Post
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/posts/root-1/thread"):
			getCalls.Add(1)
			postList := &mattermostmodel.PostList{Posts: map[string]*mattermostmodel.Post{}, Order: []string{}}
			createdMu.Lock()
			if visible.Load() && created != nil {
				postList.Posts[created.Id] = created.Clone()
				postList.Order = append(postList.Order, created.Id)
			}
			createdMu.Unlock()
			if err := json.NewEncoder(writer).Encode(postList); err != nil {
				t.Errorf("закодировать Mattermost thread response: %v", err)
			}
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/posts"):
			postCalls.Add(1)
			var post mattermostmodel.Post
			if err := json.NewDecoder(request.Body).Decode(&post); err != nil {
				t.Errorf("декодировать Mattermost create request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			post.Id = "late-attention-post"
			post.CreateAt = 1_000
			post.Props = callbackServerProps(post.GetProps())
			createdMu.Lock()
			created = post.Clone()
			createdMu.Unlock()
			cancelRequest()
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"id":"synthetic","message":"response lost after accept","status_code":500}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	newService := func() *statusservice.AutomationService {
		return statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
			Repository:   repository,
			Catalog:      &lateVisibilityAutomationCatalog{},
			Publisher:    NewControlSurface(server.URL, "synthetic-token", ""),
			StorageReady: true,
			Now:          func() time.Time { return now },
		})
	}
	command := statusservice.AutomationCallbackCommand{
		RunPublicID: runID, AuthenticatedProjectID: 1, AuthenticatedSessionID: 2, AuthenticatedSessionKey: "session-4",
		CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeRequiresHuman),
		AgentSummary: "Нужно решение", ExactPayload: []byte(`{"outcome":"requires_human","summary":"Нужно решение"}`),
	}

	first, err := newService().CompleteCallback(requestCtx, command)
	if err != nil || first.DeliveryStatus != "pending" || first.NextAction != "retry_same_callback" {
		t.Fatalf("первая неоднозначная доставка=%#v error=%v", first, err)
	}
	if !errors.Is(requestCtx.Err(), context.Canceled) {
		t.Fatalf("request context не отменён после принятого POST: %v", requestCtx.Err())
	}
	if postCalls.Load() != 1 || getCalls.Load() != 1 {
		t.Fatalf("первый transport contour: GET=%d POST=%d", getCalls.Load(), postCalls.Load())
	}
	retained := repository.snapshot()
	if !retained.ConfirmationPending || retained.ClaimToken == "" || !retained.LeaseExpiresAt.Equal(start.Add(30*time.Second)) || repository.deferCalls != 0 || repository.retainCalls != 2 {
		t.Fatalf("неоднозначный claim не удержан: delivery=%#v defer=%d retain=%d", retained, repository.deferCalls, repository.retainCalls)
	}

	now = start.Add(5 * time.Second)
	type concurrentResult struct {
		callback  statusservice.AutomationCallbackResult
		delivered int
		err       error
	}
	results := make(chan concurrentResult, 2)
	var retries sync.WaitGroup
	retries.Add(2)
	go func() {
		defer retries.Done()
		callback, retryErr := newService().CompleteCallback(context.Background(), command)
		results <- concurrentResult{callback: callback, err: retryErr}
	}()
	go func() {
		defer retries.Done()
		delivered, retryErr := newService().ReconcileOwnerAttentionDeliveries(context.Background(), 1)
		results <- concurrentResult{delivered: delivered, err: retryErr}
	}()
	retries.Wait()
	close(results)
	for result := range results {
		if result.err != nil || result.delivered != 0 || (result.callback.Run.ID != 0 && (!result.callback.Duplicate || result.callback.DeliveryStatus != "pending")) {
			t.Fatalf("конкурентный retry=%#v", result)
		}
	}
	if postCalls.Load() != 1 || getCalls.Load() != 1 {
		t.Fatalf("конкурентный retry пересёк внешний transport: GET=%d POST=%d", getCalls.Load(), postCalls.Load())
	}

	now = start.Add(31 * time.Second)
	delivered, err := newService().ReconcileOwnerAttentionDeliveries(context.Background(), 1)
	if err == nil || delivered != 0 || postCalls.Load() != 1 || getCalls.Load() != 2 {
		t.Fatalf("невидимый принятый post: delivered=%d error=%v GET=%d POST=%d", delivered, err, getCalls.Load(), postCalls.Load())
	}
	quarantined := repository.snapshot()
	if !quarantined.ConfirmationPending || quarantined.MattermostPostID != "" {
		t.Fatalf("невидимая доставка вышла из confirmation-only: %#v", quarantined)
	}

	visible.Store(true)
	now = start.Add(62 * time.Second)
	delivered, err = newService().ReconcileOwnerAttentionDeliveries(context.Background(), 1)
	if err != nil || delivered != 1 {
		t.Fatalf("позднее подтверждение: delivered=%d error=%v", delivered, err)
	}
	confirmed := repository.snapshot()
	if confirmed.MattermostPostID != "late-attention-post" || confirmed.MattermostPostCreateAt != 1_000 || confirmed.ConfirmationPending || confirmed.ClaimToken != "" || postCalls.Load() != 1 || getCalls.Load() != 3 {
		t.Fatalf("итог позднего подтверждения: delivery=%#v GET=%d POST=%d", confirmed, getCalls.Load(), postCalls.Load())
	}
}

type lateVisibilityAutomationCatalog struct {
	statusservice.AutomationCatalog
}

type lateVisibilityAutomationRepository struct {
	automationsrepo.Repository
	mu           sync.Mutex
	run          entity.ScheduledRun
	gateContext  entity.AutomationOwnerGateContext
	delivery     entity.AutomationOwnerAttentionDelivery
	accepted     bool
	payloadHash  []byte
	deferCalls   int
	retainCalls  int
	setPostCalls int
}

func (repository *lateVisibilityAutomationRepository) GetOwnerGateContext(context.Context, automationsrepo.OwnerGateContextInput) (entity.AutomationOwnerGateContext, error) {
	return repository.gateContext, nil
}

func (repository *lateVisibilityAutomationRepository) CompleteCallback(_ context.Context, input automationsrepo.CompleteCallbackInput) (entity.ScheduledRun, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.accepted {
		if !bytes.Equal(repository.payloadHash, input.PayloadSHA256) {
			return entity.ScheduledRun{}, false, automationsrepo.ErrCallbackMismatch
		}
		return repository.run, true, nil
	}
	if input.OwnerGate == nil {
		return entity.ScheduledRun{}, false, errors.New("owner gate plan is missing")
	}
	repository.accepted = true
	repository.payloadHash = append([]byte(nil), input.PayloadSHA256...)
	repository.run.Status = input.Status
	repository.run.Outcome = input.Outcome
	repository.delivery = entity.AutomationOwnerAttentionDelivery{
		AttentionID: 81, ScheduledRunID: repository.run.ID, ScheduledRunPublicID: repository.run.PublicID,
		ProcessRunID: input.OwnerGate.ProcessRunID, PolicyRevisionID: input.OwnerGate.PolicyRevisionID,
		RootInitiatorUserID: input.OwnerGate.RootInitiatorUserID,
		MattermostChannelID: repository.gateContext.MattermostChannelID, MattermostRootPostID: repository.gateContext.MattermostRootPostID,
		Status: "open", DeliveryID: input.OwnerGate.DeliveryID, DeliveryMessage: input.OwnerGate.DeliveryMessage,
		DeliveryPropsJSON: append([]byte(nil), input.OwnerGate.DeliveryPropsJSON...), DeliveryPayloadSHA256: append([]byte(nil), input.OwnerGate.DeliveryPayloadSHA256...),
	}
	return repository.run, false, nil
}

func (repository *lateVisibilityAutomationRepository) GetOwnerAttentionDelivery(context.Context, int64) (entity.AutomationOwnerAttentionDelivery, error) {
	return repository.snapshot(), nil
}

func (repository *lateVisibilityAutomationRepository) ClaimOwnerAttentionDelivery(_ context.Context, input automationsrepo.ClaimOwnerAttentionDeliveryInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.AttentionID == 0 || repository.delivery.MattermostPostID != "" || (input.ScheduledRunID > 0 && input.ScheduledRunID != repository.delivery.ScheduledRunID) || (repository.delivery.ClaimToken != "" && repository.delivery.LeaseExpiresAt.After(input.Now)) {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrNotFound
	}
	repository.delivery.ClaimToken = input.ClaimToken
	repository.delivery.ClaimedAt = input.Now
	repository.delivery.LeaseExpiresAt = input.LeaseUntil
	repository.delivery.Fence++
	return repository.delivery, nil
}

func (repository *lateVisibilityAutomationRepository) DeferOwnerAttentionDelivery(_ context.Context, input automationsrepo.DeferOwnerAttentionDeliveryInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.ClaimToken != input.ClaimToken || repository.delivery.Fence != input.Fence || repository.delivery.ConfirmationPending {
		return automationsrepo.ErrConflict
	}
	repository.deferCalls++
	repository.delivery.ClaimToken = ""
	repository.delivery.ClaimedAt = time.Time{}
	repository.delivery.LeaseExpiresAt = time.Time{}
	return nil
}

func (repository *lateVisibilityAutomationRepository) RetainOwnerAttentionDelivery(ctx context.Context, input automationsrepo.RetainOwnerAttentionDeliveryInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.ClaimToken != input.ClaimToken || repository.delivery.Fence != input.Fence || !input.LeaseUntil.After(input.Now) {
		return automationsrepo.ErrConflict
	}
	repository.retainCalls++
	repository.delivery.ConfirmationPending = true
	repository.delivery.LeaseExpiresAt = input.LeaseUntil
	return nil
}

func (repository *lateVisibilityAutomationRepository) SetOwnerAttentionPost(_ context.Context, input automationsrepo.SetOwnerAttentionPostInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.ClaimToken != input.ClaimToken || repository.delivery.Fence != input.Fence || repository.delivery.DeliveryID != input.DeliveryID || repository.delivery.MattermostChannelID != input.MattermostChannelID || repository.delivery.MattermostRootPostID != input.MattermostRootPostID {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrConflict
	}
	repository.setPostCalls++
	repository.delivery.MattermostPostID = input.MattermostPostID
	repository.delivery.MattermostPostCreateAt = input.MattermostPostCreateAt
	repository.delivery.ClaimToken = ""
	repository.delivery.ClaimedAt = time.Time{}
	repository.delivery.LeaseExpiresAt = time.Time{}
	repository.delivery.ConfirmationPending = false
	return repository.delivery, nil
}

func (repository *lateVisibilityAutomationRepository) snapshot() entity.AutomationOwnerAttentionDelivery {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.delivery
}
