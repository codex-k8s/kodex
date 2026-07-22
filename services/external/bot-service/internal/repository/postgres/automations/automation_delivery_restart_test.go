//go:build postgres

package automations_test

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

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	mattermostintegration "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/mattermost"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestMigration36QuarantinesLegacyOpenPostBindingForCreateAtReconciliation(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "automation_delivery_migration")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 35); err != nil {
		t.Fatalf("применить схему 35: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть PostgreSQL: %v", err)
	}
	defer pool.Close()
	now := time.Date(2026, time.July, 22, 15, 0, 0, 0, time.UTC)
	run, callback := prepareRestartDeliveryGate(t, ctx, pool, now)
	legacyService := statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository: postgresrepo.NewRepository(pool), Catalog: &ownerGateCatalogStub{}, StorageReady: true,
		Now: func() time.Time { return now },
	})
	if result, completeErr := legacyService.CompleteCallback(ctx, callback); completeErr != nil || result.DeliveryStatus != "pending" {
		t.Fatalf("создать legacy attention: result=%#v error=%v", result, completeErr)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_owner_attention_requests set mattermost_post_id = 'legacy-attention-post' where automation_scheduled_run_id = $1`, run.ID); err != nil {
		t.Fatalf("смоделировать legacy binding без CreateAt: %v", err)
	}

	if err := migrations.RunTo(ctx, dsn, 36); err != nil {
		t.Fatalf("обновить схему 35 -> 36: %v", err)
	}
	if version, err := migrations.Version(ctx, dsn); err != nil || version != 36 {
		t.Fatalf("версия после обновления=%d error=%v", version, err)
	}
	var postID string
	var confirmationPending bool
	var claimToken *string
	if err := pool.QueryRow(ctx, `select mattermost_post_id, automation_delivery_confirmation_pending, automation_delivery_claim_token from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&postID, &confirmationPending, &claimToken); err != nil {
		t.Fatal(err)
	}
	if postID != "" || !confirmationPending || claimToken == nil || !strings.HasPrefix(*claimToken, "migration-36-confirm-") {
		t.Fatalf("legacy binding не переведён в confirmation-only: post=%q pending=%t claim=%v", postID, confirmationPending, claimToken != nil)
	}
	claimNow := time.Now().UTC().Add(2 * time.Second)
	claimed, err := postgresrepo.NewRepository(pool).ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
		ScheduledRunID: run.ID, ClaimToken: "claim-after-migration-36", Now: claimNow,
		LeaseUntil: claimNow.Add(30 * time.Second), EligibleBefore: claimNow,
	})
	if err != nil || !claimed.ConfirmationPending {
		t.Fatalf("legacy binding не доступен confirmation-only worker: delivery=%#v error=%v", claimed, err)
	}
}

func TestAutomationDeliveryPersistentFenceSurvivesPostCommitCrashAndRestart(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "automation_delivery_restart")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("применить актуальные миграции: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть PostgreSQL: %v", err)
	}
	defer pool.Close()

	start := time.Date(2026, time.July, 22, 16, 0, 0, 0, time.UTC)
	run, callback := prepareRestartDeliveryGate(t, ctx, pool, start)

	var visible atomic.Bool
	var fenceObserved atomic.Bool
	var getCalls atomic.Int64
	var postCalls atomic.Int64
	var createdMu sync.Mutex
	var created *mattermostmodel.Post
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/posts/restart-root/thread"):
			getCalls.Add(1)
			postList := &mattermostmodel.PostList{Posts: map[string]*mattermostmodel.Post{}, Order: []string{}}
			createdMu.Lock()
			if visible.Load() && created != nil {
				postList.Posts[created.Id] = created.Clone()
				postList.Order = append(postList.Order, created.Id)
			}
			createdMu.Unlock()
			if encodeErr := json.NewEncoder(writer).Encode(postList); encodeErr != nil {
				t.Errorf("закодировать Mattermost thread: %v", encodeErr)
			}
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/posts"):
			postCalls.Add(1)
			var confirmationPending bool
			if queryErr := pool.QueryRow(request.Context(), `select automation_delivery_confirmation_pending from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&confirmationPending); queryErr != nil || !confirmationPending {
				t.Errorf("POST пересёк transport до persistent fence: pending=%t error=%v", confirmationPending, queryErr)
				writer.WriteHeader(http.StatusInternalServerError)
				return
			}
			fenceObserved.Store(true)
			var post mattermostmodel.Post
			if decodeErr := json.NewDecoder(request.Body).Decode(&post); decodeErr != nil {
				t.Errorf("декодировать Mattermost POST: %v", decodeErr)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			post.Id = "restart-attention-post"
			post.CreateAt = 1_000
			if post.Props == nil {
				post.Props = map[string]any{}
			}
			post.Props[mattermostmodel.PostPropsFromBot] = "true"
			createdMu.Lock()
			created = post.Clone()
			createdMu.Unlock()
			writer.WriteHeader(http.StatusCreated)
			if encodeErr := json.NewEncoder(writer).Encode(&post); encodeErr != nil {
				t.Errorf("закодировать Mattermost POST response: %v", encodeErr)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	now := start
	persistentRepository := postgresrepo.NewRepository(pool)
	crashingRepository := &postCommitCrashRepository{Repository: persistentRepository}
	firstProcess := statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository:   crashingRepository,
		Catalog:      &ownerGateCatalogStub{},
		Publisher:    mattermostintegration.NewControlSurface(server.URL, "synthetic-token", ""),
		StorageReady: true,
		Now:          func() time.Time { return now },
	})

	first, err := firstProcess.CompleteCallback(ctx, callback)
	if err != nil || first.DeliveryStatus != "pending" || first.NextAction != "retry_same_callback" {
		t.Fatalf("первый процесс после принятого POST: result=%#v error=%v", first, err)
	}
	if postCalls.Load() != 1 || getCalls.Load() != 1 || !fenceObserved.Load() || crashingRepository.setPostCalls.Load() != 1 || crashingRepository.retainCalls.Load() != 2 {
		t.Fatalf("граница сбоя: POST=%d GET=%d set=%d retain=%d", postCalls.Load(), getCalls.Load(), crashingRepository.setPostCalls.Load(), crashingRepository.retainCalls.Load())
	}

	// Новый repository читает только долговечное состояние; состояние первого service не переиспользуется.
	restartedRepository := postgresrepo.NewRepository(pool)
	quarantined, err := restartedRepository.GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || !quarantined.ConfirmationPending || quarantined.MattermostPostID != "" || quarantined.ClaimToken == "" || quarantined.Fence != 1 {
		t.Fatalf("persistent confirmation-only после restart: delivery=%#v error=%v", quarantined, err)
	}

	now = start.Add(31 * time.Second)
	secondProcess := statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository:   restartedRepository,
		Catalog:      &ownerGateCatalogStub{},
		Publisher:    mattermostintegration.NewControlSurface(server.URL, "synthetic-token", ""),
		StorageReady: true,
		Now:          func() time.Time { return now },
	})
	delivered, err := secondProcess.ReconcileOwnerAttentionDeliveries(ctx, 1)
	if err == nil || delivered != 0 || postCalls.Load() != 1 || getCalls.Load() != 2 {
		t.Fatalf("невидимый post после reclaim: delivered=%d error=%v POST=%d GET=%d", delivered, err, postCalls.Load(), getCalls.Load())
	}
	stillQuarantined, err := postgresrepo.NewRepository(pool).GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || !stillQuarantined.ConfirmationPending || stillQuarantined.MattermostPostID != "" || stillQuarantined.Fence != 2 {
		t.Fatalf("reclaim потерял quarantine: delivery=%#v error=%v", stillQuarantined, err)
	}

	visible.Store(true)
	now = start.Add(62 * time.Second)
	thirdProcess := statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository:   postgresrepo.NewRepository(pool),
		Catalog:      &ownerGateCatalogStub{},
		Publisher:    mattermostintegration.NewControlSurface(server.URL, "synthetic-token", ""),
		StorageReady: true,
		Now:          func() time.Time { return now },
	})
	delivered, err = thirdProcess.ReconcileOwnerAttentionDeliveries(ctx, 1)
	if err != nil || delivered != 1 {
		t.Fatalf("позднее подтверждение после restart: delivered=%d error=%v", delivered, err)
	}
	confirmed, err := postgresrepo.NewRepository(pool).GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || confirmed.MattermostPostID != "restart-attention-post" || confirmed.MattermostPostCreateAt != 1_000 || confirmed.ConfirmationPending || confirmed.ClaimToken != "" || postCalls.Load() != 1 || getCalls.Load() != 3 {
		t.Fatalf("итоговая доставка: delivery=%#v error=%v POST=%d GET=%d", confirmed, err, postCalls.Load(), getCalls.Load())
	}
}

type postCommitCrashRepository struct {
	automationsrepo.Repository
	retainCalls  atomic.Int64
	setPostCalls atomic.Int64
}

func (repository *postCommitCrashRepository) RetainOwnerAttentionDelivery(ctx context.Context, input automationsrepo.RetainOwnerAttentionDeliveryInput) error {
	if repository.retainCalls.Add(1) == 1 {
		return repository.Repository.RetainOwnerAttentionDelivery(ctx, input)
	}
	return errors.New("synthetic database failure after accepted Mattermost POST")
}

func (repository *postCommitCrashRepository) SetOwnerAttentionPost(context.Context, automationsrepo.SetOwnerAttentionPostInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.setPostCalls.Add(1)
	return entity.AutomationOwnerAttentionDelivery{}, errors.New("synthetic process crash before post binding")
}

func prepareRestartDeliveryGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) (entity.ScheduledRun, statusservice.AutomationCallbackCommand) {
	t.Helper()
	var projectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Restart delivery project', 'restart-delivery-project') returning id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'worker', 'worker', true) returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'restart-channel', 'Restart delivery chat', 'restart-delivery-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	repository := postgresrepo.NewRepository(pool)
	schedule, created, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: "schedule-88888888888888888888888888888888", ProjectID: projectID, TargetAgentRoleID: roleID, TargetChatID: chatID,
		Name: "Restart delivery schedule", OwnerMattermostUserID: "schedule-owner-id", OwnerMattermostUserName: "schedule-owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC", NextRunAt: now.Add(time.Hour),
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1, PromptSnapshot: "saved",
		PromptSHA256: bytes.Repeat([]byte{0x41}, 32), CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey: "restart-delivery-schedule", CommandHash: bytes.Repeat([]byte{0x42}, 32), Now: now,
	})
	if err != nil || !created {
		t.Fatalf("создать расписание: created=%t error=%v", created, err)
	}
	run, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		IdempotencyKey: "restart-delivery-run", OccurrencePublicID: "occurrence-88888888888888888888888888888888",
		RunPublicID: "scheduled-run-88888888888888888888888888888888", ScheduledFor: now,
		CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: "runtime-restart-delivery",
	})
	if err != nil || !created {
		t.Fatalf("создать запуск: created=%t error=%v", created, err)
	}
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		MattermostChannelID: "restart-channel", MattermostRootPostID: "restart-root", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	wrongSessionID, wrongTurnID := createRuntimeBindingInChannel(t, ctx, pool, projectID, roleID, chatID, "restart-wrong-scope", "runtime-restart-delivery", "wrong-restart-channel", "restart-root", now.Add(time.Hour))
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		RuntimeSessionID: wrongSessionID, RuntimeSessionKey: "restart-wrong-scope", RuntimeTurnID: wrongTurnID, RuntimeRunID: "runtime-restart-delivery",
		MattermostChannelID: "restart-channel", MattermostRootPostID: "restart-root", Now: now,
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("несовпадающий runtime channel scope принят: %v", err)
	}
	unbound, err := repository.GetRun(ctx, run.PublicID, projectID, "schedule-owner-id")
	if err != nil || unbound.RuntimeSessionID != 0 || unbound.RuntimeTurnID != 0 {
		t.Fatalf("отклонённый runtime scope частично привязал запуск: run=%#v error=%v", unbound, err)
	}
	sessionID, turnID := createRuntimeBindingInChannel(t, ctx, pool, projectID, roleID, chatID, "restart-root", "runtime-restart-delivery", "restart-channel", "restart-root", now.Add(time.Hour))
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: "restart-root", RuntimeTurnID: turnID, RuntimeRunID: "runtime-restart-delivery",
		MattermostChannelID: "restart-channel", MattermostRootPostID: "restart-root", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := adminpostgres.NewRepository(pool).EnsureTurnProcess(ctx, adminrepo.EnsureTurnProcessInput{
		TurnID: turnID, ProjectID: projectID, RoleID: roleID,
		InitiatorUserID: "root-owner-id", InitiatorUserName: "root-owner", TriggerPostID: "restart-root",
		MattermostChannelID: "restart-channel", MattermostRootPostID: "restart-root",
	}); err != nil {
		t.Fatalf("создать process context: %v", err)
	}
	return run, statusservice.AutomationCallbackCommand{
		RunPublicID: run.PublicID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: sessionID, AuthenticatedSessionKey: "restart-root",
		CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeRequiresHuman),
		AgentSummary: "Нужно решение", ExactPayload: []byte(`{"outcome":"requires_human","summary":"Нужно решение"}`),
	}
}
