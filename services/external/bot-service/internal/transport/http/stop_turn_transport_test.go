package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
				if test.requireGuard && len(store.guardInputs) != 4 {
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
	store.denyGuardAt = 4
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
	if recorder.Code != http.StatusUnauthorized || repository.issues != 1 || len(store.guardInputs) != 4 {
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
