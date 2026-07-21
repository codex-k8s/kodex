package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

func TestAgentSessionListActiveWorkUsesProjectScope(t *testing.T) {
	base, runner, _ := agentSessionStatusTestDeps()
	store := &fakeCoordinationStore{
		fakeAdminStore: base,
		capabilities: map[string]bool{
			entity.CoordinationCapabilityReadProjectWork: true,
		},
		claims: []entity.WorkClaim{{ID: 1, ProcessRunID: 7, TurnID: 11, RoleID: 2, Status: "active"}},
	}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store: store, RuntimeRunner: runner, StorageReady: true, RuntimeReady: true,
	})

	result, err := svc.ListActiveWork(context.Background(), "session-1", "session-token", 20)
	if err != nil {
		t.Fatalf("ListActiveWork() error = %v", err)
	}
	if store.listProcessRunID != 0 || store.listProjectID != 1 {
		t.Fatalf("ListActiveWork() scope process=%d project=%d", store.listProcessRunID, store.listProjectID)
	}
	if len(result.Claims) != 1 || result.Claims[0].TurnID != 11 {
		t.Fatalf("ListActiveWork() result = %#v", result)
	}
}

func TestAgentSessionUpdateWorkContextRequiresOwnWorkCapability(t *testing.T) {
	base, runner, _ := agentSessionStatusTestDeps()
	session := base.agentSessions["session-1"]
	session.ActiveTurnID = 1
	base.agentSessions["session-1"] = session
	store := &fakeCoordinationStore{fakeAdminStore: base, capabilities: map[string]bool{}}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store: store, RuntimeRunner: runner, StorageReady: true, RuntimeReady: true,
	})

	_, err := svc.UpdateWorkContext(context.Background(), "session-1", "session-token", AgentSessionWorkContextCommand{Summary: "Работа"})
	if err == nil || !strings.Contains(err.Error(), entity.CoordinationCapabilityUpdateOwnWork) {
		t.Fatalf("UpdateWorkContext() denied error = %v", err)
	}
	if store.updatedClaim.TurnID != 0 {
		t.Fatalf("UpdateWorkContext() unexpectedly updated claim = %#v", store.updatedClaim)
	}

	store.capabilities[entity.CoordinationCapabilityUpdateOwnWork] = true
	claim, err := svc.UpdateWorkContext(context.Background(), "session-1", "session-token", AgentSessionWorkContextCommand{
		Summary: " Работа ", Domains: []string{"runtime", "runtime", ""},
	})
	if err != nil {
		t.Fatalf("UpdateWorkContext() allowed error = %v", err)
	}
	if claim.TurnID != 1 || store.updatedClaim.Summary != "Работа" || len(store.updatedClaim.Domains) != 1 {
		t.Fatalf("UpdateWorkContext() claim = %#v input = %#v", claim, store.updatedClaim)
	}
}

func TestAgentSessionMemoryRejectsLikelySecretAssignment(t *testing.T) {
	base, runner, _ := agentSessionStatusTestDeps()
	store := &fakeCoordinationStore{
		fakeAdminStore: base,
		capabilities: map[string]bool{
			entity.CoordinationCapabilityWriteRoleMemory: true,
		},
	}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store: store, RuntimeRunner: runner, StorageReady: true, RuntimeReady: true,
	})

	_, err := svc.RememberMemory(context.Background(), "session-1", "session-token", AgentSessionMemoryRememberCommand{
		Scope: "role", Title: "Доступ", Content: "API_KEY=do-not-store", Importance: "normal",
	})
	if err == nil || !strings.Contains(err.Error(), "likely secret") {
		t.Fatalf("RememberMemory() error = %v", err)
	}
}

func TestCoordinationPermissionRequiresCapabilityAndRelationship(t *testing.T) {
	store := &fakeCoordinationStore{
		fakeAdminStore: &fakeAdminStore{},
		capabilities:   map[string]bool{entity.CoordinationCapabilityStartAgents: true},
		relationships:  map[string]bool{},
	}
	svc := NewAgentSessionService(AgentSessionServiceConfig{Store: store})
	session := entity.AgentSession{ActiveTurnID: 10, ProjectID: 1, RoleID: 2}

	err := svc.requireCoordinationPermission(context.Background(), session, entity.CoordinationCapabilityStartAgents, entity.CoordinationActionStart, 3)
	if err == nil || !strings.Contains(err.Error(), "denies action") {
		t.Fatalf("requireCoordinationPermission() relationship error = %v", err)
	}
	store.relationships[coordinationRelationshipKey(entity.CoordinationActionStart, 3)] = true
	if err := svc.requireCoordinationPermission(context.Background(), session, entity.CoordinationCapabilityStartAgents, entity.CoordinationActionStart, 3); err != nil {
		t.Fatalf("requireCoordinationPermission() allowed error = %v", err)
	}
}

func TestQueuedTurnForProcessDoesNotMixRootProcesses(t *testing.T) {
	store := &fakeCoordinationStore{
		fakeAdminStore: &fakeAdminStore{},
		processes: map[int64]entity.ProcessContext{
			10: {ProcessRunID: 1},
			20: {ProcessRunID: 2},
			21: {ProcessRunID: 1},
		},
	}
	svc := NewAgentSessionService(AgentSessionServiceConfig{Store: store})
	turn, compatible, err := svc.queuedTurnForProcess(context.Background(), 10, []entity.AgentSessionTurn{{ID: 20}, {ID: 21}})
	if err != nil || !compatible || turn.ID != 21 {
		t.Fatalf("queuedTurnForProcess() turn=%#v compatible=%t error=%v", turn, compatible, err)
	}
	_, compatible, err = svc.queuedTurnForProcess(context.Background(), 10, []entity.AgentSessionTurn{{ID: 20}})
	if err != nil || compatible {
		t.Fatalf("queuedTurnForProcess() cross-process compatible=%t error=%v", compatible, err)
	}
}

func TestSafeFailureSummaryRedactsSensitiveLinesAndTruncates(t *testing.T) {
	value := "first line\nauthorization: bearer secret-value\n" + strings.Repeat("x", 1400)
	result := safeFailureSummary(value)
	if strings.Contains(result, "secret-value") || !strings.Contains(result, "[скрыто: потенциальный секрет]") {
		t.Fatalf("safeFailureSummary() = %q", result)
	}
	if len([]rune(result)) > 1203 {
		t.Fatalf("safeFailureSummary() length = %d", len([]rune(result)))
	}
}

func TestSafeFailureSummaryKeepsOnlyKeyedFallbackRedaction(t *testing.T) {
	fixtures := map[string]string{
		"OPENAI_API_KEY":                   "mc-sentinel-openai-mattermost-0de13d2c",
		"GH_TOKEN":                         "mc-sentinel-github-mattermost-23e25df0",
		"MATTERCODEX_MATTERMOST_BOT_TOKEN": "mc-sentinel-mattermost-payload-870877bc",
		"KUBERNETES_BEARER_TOKEN":          "mc-sentinel-kubernetes-payload-8178f233",
		"MATTERCODEX_DATABASE_DSN":         "postgres://mc-sentinel-postgres-payload-a3b33c1e@127.0.0.1/disposable",
		"MATTERCODEX_SESSION_TOKEN":        "mc-sentinel-session-payload-a7076e4c",
		"MATTERCODEX_MCP_TOKEN":            "mc-sentinel-mcp-payload-42f4eb88",
	}
	lines := []string{"controlled fault before provider effect"}
	for name, value := range fixtures {
		lines = append(lines, name+"="+value)
	}
	result := safeFailureSummary(strings.Join(lines, "\n"))
	if !strings.Contains(result, "controlled fault before provider effect") {
		t.Fatalf("безопасная причина отказа потеряна: %q", result)
	}
	for class, value := range fixtures {
		if strings.Contains(result, value) {
			t.Fatalf("Mattermost payload содержит значение класса %s", class)
		}
	}
}

type fakeCoordinationStore struct {
	*fakeAdminStore
	capabilities            map[string]bool
	relationships           map[string]bool
	claims                  []entity.WorkClaim
	processes               map[int64]entity.ProcessContext
	processErr              error
	listProcessRunID        int64
	listProjectID           int64
	updatedClaim            adminrepo.UpdateWorkClaimInput
	updatedClaims           []adminrepo.UpdateWorkClaimInput
	updateWorkClaimErr      error
	reconcileErr            error
	reconcileCalls          int
	ownerAttention          entity.OwnerAttentionRequest
	ownerAttentionInput     adminrepo.CreateOwnerAttentionInput
	createOwnerAttentionErr error
	setOwnerAttentionErr    error
}

func (store *fakeCoordinationStore) WithExactAgentSessionsRuntimeGuard(ctx context.Context, expected []entity.AgentSession, sideEffect func(adminrepo.Repository) error) error {
	return store.fakeAdminStore.WithExactAgentSessionsRuntimeGuard(ctx, expected, func(adminrepo.Repository) error {
		return sideEffect(store)
	})
}

func (store *fakeCoordinationStore) EnsureTurnProcess(context.Context, adminrepo.EnsureTurnProcessInput) (entity.ProcessContext, error) {
	return entity.ProcessContext{}, nil
}

func (store *fakeCoordinationStore) GetTurnProcess(_ context.Context, turnID int64) (entity.ProcessContext, error) {
	if store.processErr != nil {
		return entity.ProcessContext{}, store.processErr
	}
	process, ok := store.processes[turnID]
	if !ok {
		return entity.ProcessContext{}, adminrepo.ErrNotFound
	}
	return process, nil
}

func (store *fakeCoordinationStore) GetTurnLineage(context.Context, int64) ([]entity.ProcessLineageStep, error) {
	return nil, adminrepo.ErrNotFound
}

func (store *fakeCoordinationStore) IsRoleCapabilityAllowed(_ context.Context, _ int64, _ int64, _ int64, capability string) (bool, error) {
	return store.capabilities[capability], nil
}

func (store *fakeCoordinationStore) IsRoleRelationshipAllowed(_ context.Context, _ int64, _ int64, _ int64, action string, targetRoleID int64) (bool, error) {
	return store.relationships[coordinationRelationshipKey(action, targetRoleID)], nil
}

func (store *fakeCoordinationStore) UpdateWorkClaim(_ context.Context, input adminrepo.UpdateWorkClaimInput) (entity.WorkClaim, error) {
	store.updatedClaim = input
	store.updatedClaims = append(store.updatedClaims, input)
	if store.updateWorkClaimErr != nil {
		return entity.WorkClaim{}, store.updateWorkClaimErr
	}
	return entity.WorkClaim{TurnID: input.TurnID, Summary: input.Summary, Domains: input.Domains}, nil
}

func (store *fakeCoordinationStore) ListActiveWork(_ context.Context, processRunID int64, projectID int64, _ int) ([]entity.WorkClaim, error) {
	store.listProcessRunID = processRunID
	store.listProjectID = projectID
	return store.claims, nil
}

func (store *fakeCoordinationStore) RememberMemory(context.Context, adminrepo.RememberMemoryInput) (entity.MemoryRecord, error) {
	return entity.MemoryRecord{}, nil
}

func (store *fakeCoordinationStore) SearchMemory(context.Context, adminrepo.SearchMemoryInput) ([]entity.MemoryRecord, error) {
	return nil, nil
}

func (store *fakeCoordinationStore) CreateOwnerAttention(_ context.Context, input adminrepo.CreateOwnerAttentionInput) (entity.OwnerAttentionRequest, bool, error) {
	store.ownerAttentionInput = input
	if store.createOwnerAttentionErr != nil {
		return entity.OwnerAttentionRequest{}, false, store.createOwnerAttentionErr
	}
	if store.ownerAttention.ID != 0 {
		return store.ownerAttention, false, nil
	}
	store.ownerAttention = entity.OwnerAttentionRequest{ID: 1, ProcessRunID: input.ProcessRunID, TurnID: input.TurnID}
	return store.ownerAttention, true, nil
}

func (store *fakeCoordinationStore) SetOwnerAttentionPost(_ context.Context, _ int64, postID string) (entity.OwnerAttentionRequest, error) {
	if store.setOwnerAttentionErr != nil {
		return entity.OwnerAttentionRequest{}, store.setOwnerAttentionErr
	}
	store.ownerAttention.MattermostPostID = postID
	return store.ownerAttention, nil
}

func (store *fakeCoordinationStore) ReconcileProcessRun(context.Context, int64) error {
	store.reconcileCalls++
	return store.reconcileErr
}

func TestRootInitiatorUserIDForTurnUsesAuthoritativeProcessBinding(t *testing.T) {
	base := chatRuntimeStore()
	base.sessionTurns = []entity.AgentSessionTurn{{ID: 7, UserID: "delegating-agent-user"}}
	store := &fakeCoordinationStore{
		fakeAdminStore: base,
		processes: map[int64]entity.ProcessContext{
			7: {RootInitiatorUserID: "owner-user"},
		},
	}
	svc := NewAgentSessionService(AgentSessionServiceConfig{Store: store})

	userID, err := svc.rootInitiatorUserIDForTurn(context.Background(), store, 7)
	if err != nil || userID != "owner-user" {
		t.Fatalf("root user id = %q, error = %v", userID, err)
	}
}

func TestRootInitiatorUserIDForTurnRejectsInvalidAuthorityState(t *testing.T) {
	tests := []struct {
		name       string
		processes  map[int64]entity.ProcessContext
		processErr error
		turnUserID string
		want       string
	}{
		{
			name: "bound process without root",
			processes: map[int64]entity.ProcessContext{
				7: {},
			},
			turnUserID: "delegating-agent-user",
			want:       "missing for process",
		},
		{
			name:       "repository failure",
			processErr: errors.New("synthetic process read failure"),
			turnUserID: "owner-user",
			want:       "synthetic process read failure",
		},
		{
			name:      "legacy turn without user",
			processes: map[int64]entity.ProcessContext{},
			want:      "missing for turn",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := chatRuntimeStore()
			base.sessionTurns = []entity.AgentSessionTurn{{ID: 7, UserID: test.turnUserID}}
			store := &fakeCoordinationStore{fakeAdminStore: base, processes: test.processes, processErr: test.processErr}
			svc := NewAgentSessionService(AgentSessionServiceConfig{Store: store})

			if _, err := svc.rootInitiatorUserIDForTurn(context.Background(), store, 7); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("root initiator error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRootInitiatorUserIDForTurnSupportsLegacyTurnWithoutProcess(t *testing.T) {
	base := chatRuntimeStore()
	base.sessionTurns = []entity.AgentSessionTurn{{ID: 7, UserID: "legacy-owner-user"}}
	store := &fakeCoordinationStore{fakeAdminStore: base, processes: map[int64]entity.ProcessContext{}}
	svc := NewAgentSessionService(AgentSessionServiceConfig{Store: store})

	userID, err := svc.rootInitiatorUserIDForTurn(context.Background(), store, 7)
	if err != nil || userID != "legacy-owner-user" {
		t.Fatalf("legacy root user id = %q, error = %v", userID, err)
	}
}

func coordinationRelationshipKey(action string, targetRoleID int64) string {
	return action + ":" + strconv.FormatInt(targetRoleID, 10)
}
