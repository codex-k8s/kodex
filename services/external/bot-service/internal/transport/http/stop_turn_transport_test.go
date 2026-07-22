package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

type stopTurnTransportStore struct {
	*fakeRouterAdminStore
	session        entity.AgentSession
	turn           entity.AgentSessionTurn
	requireGuard   bool
	denyGuard      bool
	denyGuardAt    int
	guardInputs    []securityrepo.ClusterAdminBindingInput
	cancelCalls    int
	runUpdateCalls int
}

type stopTurnTransportRunner struct {
	runtimerepo.Runner
}

type stopTurnTransportReconciler struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (reconciler *stopTurnTransportReconciler) ReconcileRuntimeTerminal(context.Context, statusservice.AutomationRuntimeTerminalCommand) error {
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	reconciler.calls++
	return reconciler.err
}

func (reconciler *stopTurnTransportReconciler) setError(err error) {
	reconciler.mu.Lock()
	reconciler.err = err
	reconciler.mu.Unlock()
}

func (reconciler *stopTurnTransportReconciler) callCount() int {
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	return reconciler.calls
}

func (stopTurnTransportRunner) InspectSecretIntegrity(context.Context, runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	return runtimerepo.SecretIntegrity{ContentSHA256: "synthetic-sha256", UID: "synthetic-uid", ResourceVersion: "1"}, nil
}

func newStopTurnTransportStore() *stopTurnTransportStore {
	return &stopTurnTransportStore{
		fakeRouterAdminStore: &fakeRouterAdminStore{},
		session: entity.AgentSession{
			ID: 1, SessionKey: "session-1", ProjectID: 1, ChatID: 1, RoleID: 1,
			MattermostChannelID: "channel-1", MattermostRootPostID: "root-1", Status: "idle",
		},
		turn: entity.AgentSessionTurn{
			ID: 1, SessionID: 1, RunID: "run-1", Status: "queued",
			MattermostChannelID: "channel-1", MattermostRootPostID: "root-1", MattermostStatusPostID: "status-post-1",
			UserID: "owner", UserName: "owner",
		},
	}
}

func (store *stopTurnTransportStore) GetAgentSession(_ context.Context, sessionKey string) (entity.AgentSession, error) {
	if sessionKey != store.session.SessionKey {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	return store.session, nil
}

func (store *stopTurnTransportStore) GetAgentSessionByID(_ context.Context, id int64) (entity.AgentSession, error) {
	if id != store.session.ID {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	return store.session, nil
}

func (store *stopTurnTransportStore) LockAgentSession(ctx context.Context, sessionKey string) (entity.AgentSession, error) {
	return store.GetAgentSession(ctx, sessionKey)
}

func (store *stopTurnTransportStore) WithExactAgentSessionsRuntimeGuard(_ context.Context, expected []entity.AgentSession, sideEffect func(adminrepo.Repository) error) error {
	if len(expected) != 1 || expected[0].ID != store.session.ID || expected[0].SessionKey != store.session.SessionKey || expected[0].ProjectID != store.session.ProjectID ||
		expected[0].ChatID != store.session.ChatID || expected[0].RoleID != store.session.RoleID || expected[0].MattermostChannelID != store.session.MattermostChannelID ||
		expected[0].MattermostRootPostID != store.session.MattermostRootPostID || expected[0].TokenSecretRef != store.session.TokenSecretRef {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect(store)
}

func (store *stopTurnTransportStore) GetAgentSessionTurn(_ context.Context, id int64) (entity.AgentSessionTurn, error) {
	if id != store.turn.ID {
		return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
	}
	return store.turn, nil
}

func (store *stopTurnTransportStore) GetAgentRole(_ context.Context, id int64) (entity.AgentRole, error) {
	if id != store.session.RoleID {
		return entity.AgentRole{}, adminrepo.ErrNotFound
	}
	access := "read-only"
	if store.requireGuard {
		access = "cluster-admin"
	}
	return entity.AgentRole{ID: id, ProjectID: 1, Name: "worker", KubernetesAccess: access, Enabled: true}, nil
}

func (store *stopTurnTransportStore) GetChat(_ context.Context, id int64) (entity.Chat, error) {
	if id != store.session.ChatID {
		return entity.Chat{}, adminrepo.ErrNotFound
	}
	return entity.Chat{ID: id, ProjectID: 1, Slug: "chat", MattermostChannelID: "channel-1"}, nil
}

func (store *stopTurnTransportStore) RequiresClusterAdminSessionGuard(context.Context, int64, string) (bool, error) {
	return store.requireGuard, nil
}

func (store *stopTurnTransportStore) WithExistingClusterAdminRuntimeGuard(_ context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func() error) error {
	store.guardInputs = append(store.guardInputs, input)
	if store.denyGuard || (store.denyGuardAt > 0 && len(store.guardInputs) == store.denyGuardAt) {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect()
}

func (store *stopTurnTransportStore) WithExistingClusterAdminPersistenceGuard(_ context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func(adminrepo.Repository) error) error {
	store.guardInputs = append(store.guardInputs, input)
	if store.denyGuard || (store.denyGuardAt > 0 && len(store.guardInputs) == store.denyGuardAt) {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect(store)
}

func (store *stopTurnTransportStore) ListClusterAdminSecretIntegrity(context.Context, int64, string) ([]securityrepo.SecretIntegrityBinding, error) {
	return []securityrepo.SecretIntegrityBinding{{
		Kind: "session", SecretRef: "session-secret", SecretKey: "token",
		ContentSHA256: "synthetic-sha256", ResourceUID: "synthetic-uid", ResourceVersion: "1",
	}}, nil
}

func (store *stopTurnTransportStore) CancelAgentSessionTurn(_ context.Context, input adminrepo.CancelAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	if input.TurnID != store.turn.ID || store.turn.Status != "queued" {
		return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
	}
	store.cancelCalls++
	store.turn.Status = "canceled"
	store.turn.Artifacts = input.Artifacts
	return store.turn, nil
}

func (store *stopTurnTransportStore) CompareAndSwapAgentSessionTurnArtifacts(_ context.Context, input adminrepo.CompareAndSwapAgentSessionTurnArtifactsInput) (entity.AgentSessionTurn, error) {
	if input.TurnID != store.turn.ID || store.turn.Status != "canceled" || input.ExpectedArtifacts != store.turn.Artifacts {
		return entity.AgentSessionTurn{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	store.turn.Artifacts = input.Artifacts
	return store.turn, nil
}

func (store *stopTurnTransportStore) UpdateAgentRunArtifacts(_ context.Context, input adminrepo.UpdateAgentRunArtifactsInput) (entity.AgentRun, error) {
	if input.RunID != store.turn.RunID {
		return entity.AgentRun{}, adminrepo.ErrNotFound
	}
	store.runUpdateCalls++
	return entity.AgentRun{RunID: input.RunID, Status: input.Status}, nil
}

func TestStopTurnProductionActionUsesAtomicTargetSessionGuard(t *testing.T) {
	tests := []struct {
		name         string
		sessionScope string
		requireGuard bool
		denyGuard    bool
		wantStatus   int
		wantEffects  bool
	}{
		{name: "ordinary control", sessionScope: "session-1", wantStatus: http.StatusOK, wantEffects: true},
		{name: "guarded cluster admin control", sessionScope: "session-1", requireGuard: true, wantStatus: http.StatusOK, wantEffects: true},
		{name: "revoked frozen target", sessionScope: "session-1", requireGuard: true, denyGuard: true, wantStatus: http.StatusUnauthorized},
		{name: "forged target session", sessionScope: "forged-session", wantStatus: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newStopTurnTransportStore()
			store.requireGuard = test.requireGuard
			store.denyGuard = test.denyGuard
			repository := &memoryInteractionRepository{
				capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{},
				mutationStore: store,
			}
			security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
				Repository: repository, Admission: fixedAdmission{status: statusservice.AdmissionAllowed},
			})
			localizer, err := texti18n.New(texti18n.DefaultLocale)
			if err != nil {
				t.Fatalf("localizer: %v", err)
			}
			publisher := &recordingThreadPublisher{}
			sessionService := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
				Localizer: localizer, Store: store, RuntimeRunner: stopTurnTransportRunner{}, ThreadPublisher: publisher, StorageReady: true,
			})
			router := NewRouter(RouterConfig{
				SessionService: sessionService, InteractionSecurity: security, Localizer: localizer, MaxSlashFormBytes: 65536,
				ThreadPublisher: statusservice.NewSecuredMattermostThreadPublisher(publisher, security),
			})
			body := stopTurnActionBody(t, router, test.sessionScope, map[string]any{
				"kind": "agent_turn", "action": "stop_turn", "turn_ids": "1",
				"resource_type": "agent_session_turn", "resource_id": "1",
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body))
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if test.wantEffects {
				if store.cancelCalls != 1 || store.runUpdateCalls != 1 || repository.consumes != 1 || len(publisher.cardUpdates) != 1 {
					t.Fatalf("normal effects cancel=%d run=%d consume=%d cards=%d", store.cancelCalls, store.runUpdateCalls, repository.consumes, len(publisher.cardUpdates))
				}
				if test.requireGuard && len(store.guardInputs) != 7 {
					t.Fatalf("guarded control guards=%#v", store.guardInputs)
				}
				return
			}
			if store.cancelCalls != 0 || store.runUpdateCalls != 0 || repository.consumes != 0 || len(publisher.cardUpdates) != 0 {
				t.Fatalf("denied effects cancel=%d run=%d consume=%d cards=%d", store.cancelCalls, store.runUpdateCalls, repository.consumes, len(publisher.cardUpdates))
			}
			if test.requireGuard {
				if len(store.guardInputs) != 1 || store.guardInputs[0].SessionKey != "session-1" || store.guardInputs[0].RoleID != 1 || store.guardInputs[0].ChatID != 1 || store.guardInputs[0].MattermostChannelID != "channel-1" {
					t.Fatalf("target guard subject=%#v", store.guardInputs)
				}
			}
		})
	}
}

func TestStopTurnProductionActionGuardsResponseCapabilityBoundary(t *testing.T) {
	store := newStopTurnTransportStore()
	store.requireGuard = true
	store.denyGuardAt = 7
	repository := &memoryInteractionRepository{
		capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}, mutationStore: store,
	}
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repository, Admission: fixedAdmission{status: statusservice.AdmissionAllowed}})
	localizer, _ := texti18n.New(texti18n.DefaultLocale)
	publisher := &recordingThreadPublisher{}
	router := NewRouter(RouterConfig{
		SessionService: statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
			Localizer: localizer, Store: store, RuntimeRunner: stopTurnTransportRunner{}, ThreadPublisher: publisher, StorageReady: true,
		}),
		InteractionSecurity: security, Localizer: localizer, MaxSlashFormBytes: 65536,
		ThreadPublisher: statusservice.NewSecuredMattermostThreadPublisher(publisher, security),
	})
	body := stopTurnActionBody(t, router, "session-1", map[string]any{
		"kind": "agent_turn", "action": "stop_turn", "turn_ids": "1", "resource_type": "agent_session_turn", "resource_id": "1",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	if recorder.Code != http.StatusUnauthorized || repository.issues != 1 || len(store.guardInputs) != 7 {
		t.Fatalf("status=%d consumes=%d issues=%d guards=%#v body=%s", recorder.Code, repository.consumes, repository.issues, store.guardInputs, recorder.Body.String())
	}
	for _, input := range store.guardInputs {
		if input.SessionKey != "session-1" || input.RoleID != 1 || input.ProjectID != 1 || input.ChatID != 1 || input.MattermostChannelID != "channel-1" {
			t.Fatalf("target guard subject=%#v", input)
		}
	}
}

func TestStopTurnProductionActionRejectsForgedTurnWithoutConsumingCapability(t *testing.T) {
	store := newStopTurnTransportStore()
	repository := &memoryInteractionRepository{
		capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}, mutationStore: store,
	}
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repository, Admission: fixedAdmission{status: statusservice.AdmissionAllowed}})
	localizer, _ := texti18n.New(texti18n.DefaultLocale)
	router := NewRouter(RouterConfig{SessionService: statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{Store: store, StorageReady: true}), InteractionSecurity: security, Localizer: localizer, MaxSlashFormBytes: 65536})
	body := stopTurnActionBody(t, router, "session-1", map[string]any{
		"kind": "agent_turn", "action": "stop_turn", "turn_ids": "2", "resource_type": "agent_session_turn", "resource_id": "1",
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	if recorder.Code != http.StatusBadRequest || store.cancelCalls != 0 || repository.consumes != 0 {
		t.Fatalf("status=%d cancel=%d consume=%d body=%s", recorder.Code, store.cancelCalls, repository.consumes, recorder.Body.String())
	}
}

func TestStopTurnProductionActionRecoversTerminalReconciliationAfterRestart(t *testing.T) {
	store := newStopTurnTransportStore()
	repository := &memoryInteractionRepository{
		capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}, mutationStore: store,
	}
	reconciler := &stopTurnTransportReconciler{}
	reconciliationErr := errors.New("synthetic terminal reconciliation failure")
	reconciler.setError(reconciliationErr)
	publisher := &recordingThreadPublisher{}
	router := newStopTurnRecoveryRouter(t, store, repository, publisher, reconciler)
	originalBody := stopTurnActionBody(t, router, "session-1", map[string]any{
		"kind": "agent_turn", "action": "stop_turn", "turn_ids": "1", "resource_type": "agent_session_turn", "resource_id": "1",
	})

	first := serveStopTurnAction(router, originalBody)
	if first.Code != http.StatusBadGateway || store.turn.Status != "canceled" || store.cancelCalls != 1 || repository.consumes != 1 || reconciler.callCount() != 1 {
		t.Fatalf("first status=%d turn=%q cancel=%d consume=%d reconcile=%d body=%s", first.Code, store.turn.Status, store.cancelCalls, repository.consumes, reconciler.callCount(), first.Body.String())
	}
	if len(publisher.cardUpdates) != 1 || len(publisher.cardUpdates[0].Actions) != 1 {
		t.Fatalf("recovery card updates=%#v issues=%d turn=%#v session=%#v", publisher.cardUpdates, repository.issues, store.turn, store.session)
	}
	recoveryBody := stopTurnBodyFromCard(t, publisher.cardUpdates[0])

	reconciler.setError(nil)
	restarted := newStopTurnRecoveryRouter(t, store, repository, publisher, reconciler)
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "wrong actor", body: mutateStopTurnBody(t, originalBody, func(payload map[string]any) { payload["user_id"] = "other-user" })},
		{name: "changed replay", body: mutateStopTurnBody(t, originalBody, func(payload map[string]any) { payload["context"].(map[string]any)["changed"] = "true" })},
		{name: "wrong project", body: stopTurnActionBodyWithScope(t, restarted, "2", "session-1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serveStopTurnAction(restarted, test.body)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	wrongSession := serveStopTurnAction(restarted, stopTurnActionBodyWithScope(t, restarted, "1", "other-session"))
	if wrongSession.Code != http.StatusUnauthorized {
		t.Fatalf("wrong session status=%d body=%s", wrongSession.Code, wrongSession.Body.String())
	}
	if reconciler.callCount() != 1 || store.cancelCalls != 1 {
		t.Fatalf("denied replay side effects cancel=%d reconcile=%d", store.cancelCalls, reconciler.callCount())
	}

	recovered := serveStopTurnAction(restarted, recoveryBody)
	if recovered.Code != http.StatusOK || store.cancelCalls != 1 || repository.consumes != 2 || reconciler.callCount() != 2 {
		t.Fatalf("recovery status=%d cancel=%d consume=%d replay=%d reconcile=%d body=%s", recovered.Code, store.cancelCalls, repository.consumes, repository.replays, reconciler.callCount(), recovered.Body.String())
	}
	cardUpdates := len(publisher.cardUpdates)
	exactReplay := serveStopTurnAction(restarted, originalBody)
	if exactReplay.Code != http.StatusUnauthorized || store.cancelCalls != 1 || repository.replays != 1 || reconciler.callCount() != 2 || len(publisher.cardUpdates) != cardUpdates {
		t.Fatalf("exact replay status=%d cancel=%d replay=%d reconcile=%d cards=%d/%d body=%s", exactReplay.Code, store.cancelCalls, repository.replays, reconciler.callCount(), len(publisher.cardUpdates), cardUpdates, exactReplay.Body.String())
	}
	secondRecovery := serveStopTurnAction(restarted, recoveryBody)
	if secondRecovery.Code != http.StatusUnauthorized || store.cancelCalls != 1 || repository.replays != 2 || reconciler.callCount() != 2 || len(publisher.cardUpdates) != cardUpdates {
		t.Fatalf("second recovery status=%d cancel=%d replay=%d reconcile=%d cards=%d/%d body=%s", secondRecovery.Code, store.cancelCalls, repository.replays, reconciler.callCount(), len(publisher.cardUpdates), cardUpdates, secondRecovery.Body.String())
	}
}

func newStopTurnRecoveryRouter(t *testing.T, store *stopTurnTransportStore, repository *memoryInteractionRepository, publisher *recordingThreadPublisher, reconciler *stopTurnTransportReconciler) *Router {
	t.Helper()
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repository, Admission: fixedAdmission{status: statusservice.AdmissionAllowed}})
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		t.Fatalf("localizer: %v", err)
	}
	securedPublisher := statusservice.NewSecuredMattermostThreadPublisher(publisher, security)
	return NewRouter(RouterConfig{
		SessionService: statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
			Localizer: localizer, Store: store, RuntimeRunner: stopTurnTransportRunner{}, ThreadPublisher: securedPublisher,
			AutomationRuntimeReconciler: reconciler, MenuActionURL: "https://mattermost.example/actions", StorageReady: true,
		}),
		InteractionSecurity: security, Localizer: localizer, MaxSlashFormBytes: 65536, ThreadPublisher: securedPublisher,
	})
}

func serveStopTurnAction(router *Router, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	return recorder
}

func stopTurnBodyFromCard(t *testing.T, card statusservice.MattermostCard) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"user_id": card.Interaction.Actor.UserID, "channel_id": card.ChannelID, "post_id": card.PostID, "context": card.Actions[0].Context,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(payload)
}

func mutateStopTurnBody(t *testing.T, body string, mutation func(map[string]any)) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	mutation(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}

func stopTurnActionBodyWithScope(t *testing.T, router *Router, workspaceScope string, sessionScope string) string {
	t.Helper()
	card := statusservice.MattermostCard{
		ChannelID: "channel-1", PostID: "post-1",
		Actions: []statusservice.MattermostCardAction{{ID: "stopturn", Context: map[string]any{
			"kind": "agent_turn", "action": "stop_turn", "turn_ids": "1", "resource_type": "agent_session_turn", "resource_id": "1",
		}}},
	}
	if err := router.interactionSecurity.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner", UserName: "owner"}, statusservice.InteractionScope{Workspace: workspaceScope, Session: sessionScope}); err != nil {
		t.Fatalf("SealCard() error = %v", err)
	}
	return stopTurnBodyFromCard(t, card)
}

func stopTurnActionBody(t *testing.T, router *Router, sessionScope string, actionContext map[string]any) string {
	t.Helper()
	card := statusservice.MattermostCard{
		ChannelID: "channel-1", PostID: "post-1",
		Actions: []statusservice.MattermostCardAction{{ID: "stopturn", Context: actionContext}},
	}
	if err := router.interactionSecurity.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner", UserName: "owner"}, statusservice.InteractionScope{Workspace: "1", Session: sessionScope}); err != nil {
		t.Fatalf("SealCard() error = %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"user_id": "owner", "user_name": "forged-name", "channel_id": "channel-1", "post_id": "post-1", "context": card.Actions[0].Context,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(payload)
}

var _ adminrepo.Repository = (*stopTurnTransportStore)(nil)
var _ securityrepo.ClusterAdminSessionSubjectRepository = (*stopTurnTransportStore)(nil)
var _ securityrepo.ClusterAdminPersistenceGuardRepository = (*stopTurnTransportStore)(nil)
var _ securityrepo.ClusterAdminRuntimeGuardRepository = (*stopTurnTransportStore)(nil)
