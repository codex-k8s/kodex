package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

func (store *fakeAdminStore) WithExactAgentSessionsRuntimeGuard(_ context.Context, expected []entity.AgentSession, sideEffect func(adminrepo.Repository) error) error {
	store.capacityMu.Lock()
	defer store.capacityMu.Unlock()
	seen := make(map[string]entity.AgentSession, len(expected))
	for _, binding := range expected {
		key := strings.TrimSpace(binding.SessionKey)
		if key == "" {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		if previous, exists := seen[key]; exists && (previous.ID != binding.ID || previous.RoleID != binding.RoleID || previous.ChatID != binding.ChatID) {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		seen[key] = binding
		current, exists := store.agentSessions[key]
		if !exists || current.ID != binding.ID || current.ProjectID != binding.ProjectID || current.ChatID != binding.ChatID || current.RoleID != binding.RoleID ||
			strings.TrimSpace(current.MattermostChannelID) != strings.TrimSpace(binding.MattermostChannelID) ||
			strings.TrimSpace(current.MattermostRootPostID) != strings.TrimSpace(binding.MattermostRootPostID) ||
			strings.TrimSpace(current.TokenSecretRef) != strings.TrimSpace(binding.TokenSecretRef) {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
	}
	return sideEffect(store)
}

func TestAgentSessionListsChatsAvailableToTargetAgent(t *testing.T) {
	svc, store, _, _ := agentDelegationTestService()
	store.chatParticipants[1] = []entity.ChatParticipant{{ChatID: 1, RoleID: 1, RoleName: "manager", Enabled: true}}
	store.chatParticipants[2] = []entity.ChatParticipant{{ChatID: 2, RoleID: 2, RoleName: "architect", Enabled: true}}

	catalog, err := svc.ListAvailableChats(context.Background(), "source-session", "source-token", "architect")
	if err != nil {
		t.Fatalf("ListAvailableChats() error = %v", err)
	}
	if len(catalog.Chats) != 1 || catalog.Chats[0].Slug != "architecture" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.TargetAgent != "architect" {
		t.Fatalf("target agent = %q", catalog.TargetAgent)
	}

	details, err := svc.ChatDetails(context.Background(), "source-session", "source-token", "architecture")
	if err != nil {
		t.Fatalf("ChatDetails() error = %v", err)
	}
	if details.Description != "Архитектурные решения" || len(details.Agents) != 1 || details.Agents[0] != "architect" {
		t.Fatalf("details = %#v", details)
	}
}

func TestAgentSessionStartsCrossChatThreadIdempotently(t *testing.T) {
	svc, store, dispatcher, publisher := agentDelegationTestService()
	command := StartAgentThreadCommand{
		TargetChat:  "architecture",
		TargetAgent: "architect",
		Title:       "Границы сервисов",
		Message:     "Подготовь предложение по границам сервисов.",
		WorkItemKey: "issue-59-architecture",
	}

	result, err := svc.StartAgentThread(context.Background(), "source-session", "source-token", command)
	if err != nil {
		t.Fatalf("StartAgentThread() error = %v", err)
	}
	if result.TargetChat != "architecture" || result.TargetAgent != "architect" || result.TargetRunID != "target-run" {
		t.Fatalf("result = %#v", result)
	}
	if result.TargetThreadURL != "https://mattermost.example/matter-codex/pl/reply-" {
		t.Fatalf("thread URL = %q", result.TargetThreadURL)
	}
	if dispatcher.calls != 1 || dispatcher.request.Chat.ID != 2 || dispatcher.request.Role.ID != 2 {
		t.Fatalf("dispatcher = %#v", dispatcher)
	}
	if dispatcher.request.SessionRootID != "reply-" || !strings.Contains(dispatcher.request.UserMessage, "mattermost_return_to_requester") {
		t.Fatalf("request = %#v", dispatcher.request)
	}
	if dispatcher.request.ParentTurnID != 1 {
		t.Fatalf("parent turn = %d", dispatcher.request.ParentTurnID)
	}
	if dispatcher.request.SourcePostID != "reply-management-root" {
		t.Fatalf("source launch post = %q", dispatcher.request.SourcePostID)
	}
	if len(publisher.posts) < 2 || publisher.posts[0].RootPostID != "" || !strings.Contains(publisher.posts[0].Message, "#notrigger") {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if publisher.posts[1].RootPostID != "management-root" || !strings.Contains(publisher.posts[1].Message, "https://mattermost.example/matter-codex/pl/reply-") {
		t.Fatalf("source audit misses target thread link: %#v", publisher.posts[1])
	}
	if len(publisher.updates) != 1 || publisher.updates[0].PostID != "reply-" || !strings.Contains(publisher.updates[0].Message, "https://mattermost.example/matter-codex/pl/reply-management-root") {
		t.Fatalf("delegated root misses exact source launch message link: %#v", publisher.updates)
	}
	if strings.Contains(publisher.updates[0].Message, "https://mattermost.example/matter-codex/pl/management-root\n") {
		t.Fatalf("delegated root still links to ambiguous source thread: %q", publisher.updates[0].Message)
	}

	second, err := svc.StartAgentThread(context.Background(), "source-session", "source-token", command)
	if err != nil {
		t.Fatalf("second StartAgentThread() error = %v", err)
	}
	if second.DelegationID != result.DelegationID || dispatcher.calls != 1 {
		t.Fatalf("second=%#v calls=%d", second, dispatcher.calls)
	}

	list, err := svc.ListDelegations(context.Background(), "source-session", "source-token", 20)
	if err != nil {
		t.Fatalf("ListDelegations() error = %v", err)
	}
	if len(list.Delegations) != 1 || list.Delegations[0].WorkItemKey != command.WorkItemKey {
		t.Fatalf("delegations = %#v", list)
	}
	if len(store.agentDelegations) != 1 {
		t.Fatalf("stored delegations = %#v", store.agentDelegations)
	}
}

func TestAgentSessionCrossChatDelegationAllowsSourceOutsideTargetChat(t *testing.T) {
	svc, store, dispatcher, _ := agentDelegationTestService()
	store.chatParticipants[2] = []entity.ChatParticipant{{ChatID: 2, RoleID: 2, RoleName: "architect", Enabled: true}}

	result, err := svc.StartAgentThread(context.Background(), "source-session", "source-token", StartAgentThreadCommand{
		TargetChat:  "architecture",
		TargetAgent: "architect",
		Title:       "Границы сервисов",
		Message:     "Подготовь предложение.",
		WorkItemKey: "issue-59-architecture",
	})
	if err != nil {
		t.Fatalf("StartAgentThread() error = %v", err)
	}
	if result.TargetAgent != "architect" || dispatcher.calls != 1 {
		t.Fatalf("result=%#v dispatcher calls=%d", result, dispatcher.calls)
	}
}

func TestAgentSessionCrossChatDelegationRequiresTargetParticipant(t *testing.T) {
	svc, store, dispatcher, _ := agentDelegationTestService()
	store.chatParticipants[2] = []entity.ChatParticipant{{ChatID: 2, RoleID: 1, RoleName: "manager", Enabled: true}}

	_, err := svc.StartAgentThread(context.Background(), "source-session", "source-token", StartAgentThreadCommand{
		TargetChat:  "architecture",
		TargetAgent: "architect",
		Title:       "Границы сервисов",
		Message:     "Подготовь предложение.",
		WorkItemKey: "issue-59-architecture",
	})
	if err == nil || !strings.Contains(err.Error(), "not available in chat") {
		t.Fatalf("error = %v", err)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("dispatcher calls = %d", dispatcher.calls)
	}
}

func TestAgentSessionReturnsCrossChatResultToImmediateRequester(t *testing.T) {
	svc, store, dispatcher, publisher := agentDelegationTestService()
	started, err := svc.StartAgentThread(context.Background(), "source-session", "source-token", StartAgentThreadCommand{
		TargetChat:  "architecture",
		TargetAgent: "architect",
		Title:       "Границы сервисов",
		Message:     "Подготовь предложение.",
		WorkItemKey: "issue-59-architecture",
	})
	if err != nil {
		t.Fatalf("StartAgentThread() error = %v", err)
	}
	store.sessionTurns = append(store.sessionTurns, entity.AgentSessionTurn{
		ID:                   3,
		SessionID:            1,
		RunID:                "callback-run",
		MattermostChannelID:  "management-channel",
		MattermostRootPostID: "management-root",
		MattermostPostID:     "management-root",
		Status:               agentSessionTurnQueued,
	})
	dispatcher.queued = AgentTurnQueued{RunID: "callback-run", TurnID: 3, SessionKey: "source-session"}

	callback, err := svc.ReturnToRequester(context.Background(), "target-session", "target-token", "Архитектурное предложение готово.")
	if err != nil {
		t.Fatalf("ReturnToRequester() error = %v", err)
	}
	if callback.DelegationID != started.DelegationID || callback.CallbackRunID != "callback-run" {
		t.Fatalf("callback = %#v", callback)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher calls = %d", dispatcher.calls)
	}
	callbackTurn, err := store.GetAgentSessionTurn(context.Background(), 3)
	if err != nil || !strings.Contains(callbackTurn.Message, "Архитектурное предложение готово") {
		t.Fatalf("callback turn = %#v error=%v", callbackTurn, err)
	}
	if !containsInt64(callbackTurn.ParentTurnIDs, 2) {
		t.Fatalf("callback parents = %#v", callbackTurn.ParentTurnIDs)
	}
	if !containsString(callbackTurn.TriggerPostIDs, "reply-") || !containsString(callbackTurn.InitiatorUserNames, "architect") {
		t.Fatalf("callback origins triggers=%#v initiators=%#v", callbackTurn.TriggerPostIDs, callbackTurn.InitiatorUserNames)
	}
	if len(publisher.posts) < 4 || !strings.Contains(publisher.posts[len(publisher.posts)-1].Message, "https://mattermost.example/matter-codex/pl/management-root") || !strings.Contains(publisher.posts[len(publisher.posts)-1].Message, "#notrigger") {
		t.Fatalf("posts = %#v", publisher.posts)
	}

	_, err = svc.ReturnToRequester(context.Background(), "target-session", "target-token", "Повторный callback.")
	if err != nil {
		t.Fatalf("second ReturnToRequester() error = %v", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher calls after duplicate callback = %d", dispatcher.calls)
	}
}

func TestReturnToRequesterGuardsFrozenSourceAndChildIndependently(t *testing.T) {
	tests := []struct {
		name          string
		frozen        map[string]bool
		denyOperation string
	}{
		{
			name:          "frozen source with ordinary child",
			frozen:        map[string]bool{"source-session": true},
			denyOperation: "agent_session.delegation_callback_persist.side_effect.source",
		},
		{
			name:          "ordinary source with frozen child",
			frozen:        map[string]bool{"target-session": true},
			denyOperation: "agent_session.delegation_callback_lookup.side_effect",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc, baseStore, dispatcher, publisher := delegationReturnBarrierFixture()
			store := &admittedAdminStore{
				fakeAdminStore: baseStore, allowed: true, frozenSessions: test.frozen,
				denyGuardOperation: test.denyOperation,
			}
			svc.cfg.Store = store
			_, err := svc.ReturnToRequester(context.Background(), "target-session", "target-token", "Синтетический результат.")
			if !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
				t.Fatalf("ReturnToRequester() error = %v", err)
			}
			turn, turnErr := baseStore.GetAgentSessionTurn(context.Background(), 3)
			if turnErr != nil || turn.Message != "Исходная очередь" || len(turn.ParentTurnIDs) != 0 {
				t.Fatalf("denied source mutation: turn=%#v error=%v", turn, turnErr)
			}
			delegation := baseStore.agentDelegations[1]
			if delegation.CallbackTurnID != 0 || delegation.CallbackRunID != "" || dispatcher.calls != 0 || len(publisher.posts) != 0 {
				t.Fatalf("denied callback effects: delegation=%#v dispatch=%d posts=%d", delegation, dispatcher.calls, len(publisher.posts))
			}
		})
	}

	t.Run("allowed frozen source control", func(t *testing.T) {
		svc, baseStore, _, _ := delegationReturnBarrierFixture()
		store := &admittedAdminStore{fakeAdminStore: baseStore, allowed: true, frozenSessions: map[string]bool{"source-session": true}}
		svc.cfg.Store = store
		result, err := svc.ReturnToRequester(context.Background(), "target-session", "target-token", "Синтетический результат.")
		if err != nil || result.CallbackRunID != "callback-run" {
			t.Fatalf("ReturnToRequester() result=%#v error=%v", result, err)
		}
		if len(store.guardInputs) == 0 {
			t.Fatal("frozen source did not use guard")
		}
		for _, input := range store.guardInputs {
			if input.SessionKey != "source-session" || input.RoleID != 1 || input.ChatID != 1 || input.MattermostChannelID != "management-channel" {
				t.Fatalf("source guard subject = %#v", input)
			}
		}
	})
}

func delegationReturnBarrierFixture() (*AgentSessionService, *fakeAdminStore, *fakeAgentTurnDispatcher, *fakeThreadPublisher) {
	svc, store, dispatcher, publisher := agentDelegationTestService()
	store.agentDelegations = map[int64]entity.AgentDelegation{1: {
		ID: 1, ProjectID: 1, SourceSessionID: 1, SourceTurnID: 1,
		TargetChatID: 2, TargetRoleID: 2, TargetRootPostID: "reply-", TargetSessionID: 2, TargetTurnID: 2,
		TargetRunID: "target-run", WorkItemKey: "synthetic-return", Title: "Синтетическая делегация", Status: agentSessionTurnRunning,
	}}
	store.sessionTurns = append(store.sessionTurns, entity.AgentSessionTurn{
		ID: 3, SessionID: 1, RunID: "callback-run", Message: "Исходная очередь",
		MattermostChannelID: "management-channel", MattermostRootPostID: "management-root", Status: agentSessionTurnQueued,
	})
	dispatcher.calls = 0
	dispatcher.queued = AgentTurnQueued{RunID: "callback-run", TurnID: 3, SessionKey: "source-session"}
	publisher.posts = nil
	publisher.updates = nil
	return svc, store, dispatcher, publisher
}

func agentDelegationTestService() (*AgentSessionService, *fakeAdminStore, *fakeAgentTurnDispatcher, *fakeThreadPublisher) {
	now := time.Now().UTC()
	store := chatRuntimeStore()
	store.projects[1] = entity.Project{ID: 1, Name: "MatterCodex", Slug: "matter-codex", MattermostTeamID: "team-1"}
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "architect", RoleType: "architect", Enabled: true}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "management-channel", Name: "Management", Slug: "management", Description: "Координация", ChatType: "manager"}
	store.chats[2] = entity.Chat{ID: 2, ProjectID: 1, MattermostChannelID: "architecture-channel", Name: "Architecture", Slug: "architecture", Description: "Архитектурные решения", ChatType: "multi_role_custom"}
	store.chatParticipants[1] = []entity.ChatParticipant{
		{ChatID: 1, RoleID: 1, RoleName: "manager", Enabled: true},
		{ChatID: 1, RoleID: 2, RoleName: "architect", Enabled: true},
	}
	store.chatParticipants[2] = []entity.ChatParticipant{
		{ChatID: 2, RoleID: 1, RoleName: "manager", Enabled: true},
		{ChatID: 2, RoleID: 2, RoleName: "architect", Enabled: true},
	}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "architect", MattermostUserID: "architect-user", Status: "configured"},
	}
	store.agentSessions = map[string]entity.AgentSession{
		"source-session": {
			ID: 1, SessionKey: "source-session", ProjectID: 1, ChatID: 1, RoleID: 1,
			SessionScope: agentSessionScopeThreadRole, MattermostChannelID: "management-channel", MattermostRootPostID: "management-root",
			Status: agentSessionStatusRunning, ActiveTurnID: 1, ActiveRunID: "source-run", TokenSecretRef: "source-secret",
			TTLSeconds: defaultThreadSessionTTLSeconds, LastActivityAt: now, ExpiresAt: now.Add(time.Hour),
		},
		"target-session": {
			ID: 2, SessionKey: "target-session", ProjectID: 1, ChatID: 2, RoleID: 2,
			SessionScope: agentSessionScopeThreadRole, MattermostChannelID: "architecture-channel", MattermostRootPostID: "reply-",
			Status: agentSessionStatusRunning, ActiveTurnID: 2, ActiveRunID: "target-run", TokenSecretRef: "target-secret",
			TTLSeconds: defaultThreadSessionTTLSeconds, LastActivityAt: now, ExpiresAt: now.Add(time.Hour),
		},
	}
	store.sessionTurns = []entity.AgentSessionTurn{
		{ID: 1, SessionID: 1, RunID: "source-run", MattermostChannelID: "management-channel", MattermostRootPostID: "management-root", MattermostPostID: "management-root", Status: agentSessionTurnRunning},
		{ID: 2, SessionID: 2, RunID: "target-run", MattermostChannelID: "architecture-channel", MattermostRootPostID: "reply-", MattermostPostID: "reply-", Status: agentSessionTurnRunning},
	}
	runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{
		"source-secret": "source-token",
		"target-secret": "target-token",
	}}
	publisher := &fakeThreadPublisher{}
	dispatcher := &fakeAgentTurnDispatcher{queued: AgentTurnQueued{RunID: "target-run", TurnID: 2, SessionKey: "target-session"}}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store:             store,
		RuntimeRunner:     runner,
		ThreadPublisher:   publisher,
		TurnDispatcher:    dispatcher,
		MattermostSiteURL: "https://mattermost.example",
		StorageReady:      true,
		RuntimeReady:      true,
	})
	return svc, store, dispatcher, publisher
}
