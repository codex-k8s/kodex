package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestChatRunIgnoresUnknownChannel(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          &fakeAdminStore{},
		RuntimeRunner:  &fakeRuntimeRunner{},
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "unknown-channel",
		PostID:    "post-1",
		UserID:    "owner",
		Message:   "Do the work",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestChatRunStartsChatModeForManagerRole(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Help me decompose the task.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey == "" || runner.sessionCodexSecret != "matter-codex-codex-auth-main" {
		t.Fatalf("session runner = %#v", runner.sessionRuns)
	}
	if len(publisher.posts) != 0 || len(publisher.cards) != 0 {
		t.Fatalf("chat handler must not create duplicate status posts, posts=%#v cards=%#v", publisher.posts, publisher.cards)
	}
	if len(store.sessionTurns) != 1 || !strings.Contains(store.sessionTurns[0].Message, "Help me decompose the task.") || !strings.Contains(store.sessionTurns[0].Message, "Проект: Platform") {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
}

func TestChatRunAddsAgentEyesReaction(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {
			ID:               1,
			ProjectID:        1,
			RoleID:           1,
			MattermostUserID: "manager-user",
			TokenSecretRef:   "manager-token-secret",
		},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"manager-token-secret": "manager-token"}}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Start working.",
	})

	if result.RunID == "" {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.reactions) != 1 {
		t.Fatalf("reactions = %#v", publisher.reactions)
	}
	reaction := publisher.reactions[0]
	if publisher.reactionTokens[0] != "manager-token" || reaction.PostID != "post-1" || reaction.UserID != "manager-user" || reaction.EmojiName != "eyes" {
		t.Fatalf("reaction token=%q input=%#v", publisher.reactionTokens[0], reaction)
	}
}

func TestChatRunDoesNotPostDuplicateQueuedTurnCard(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://matter-codex.example/mattermost/actions/agents",
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Help me decompose the task.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.cards) != 0 || len(publisher.posts) != 0 {
		t.Fatalf("publisher cards=%#v posts=%#v", publisher.cards, publisher.posts)
	}
	if len(store.sessionTurns) != 1 {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
}

func TestChatRunUsesRoleTemplateOnlyForFirstSessionTurn(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		PromptTemplate:    "BOOTSTRAP TEMPLATE: {{.Task.Body}}\nProject: {{.Project.Name}}",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	first := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "First task.",
	})
	second := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-2",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Follow-up task.",
	})

	if first.RunID == "" || second.RunID == "" {
		t.Fatalf("results = %#v %#v", first, second)
	}
	if len(store.sessionTurns) != 2 {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
	firstPrompt := store.sessionTurns[0].Message
	if !strings.Contains(firstPrompt, "BOOTSTRAP TEMPLATE: First task.") {
		t.Fatalf("first prompt = %q", firstPrompt)
	}
	secondPrompt := store.sessionTurns[1].Message
	if strings.Contains(secondPrompt, "BOOTSTRAP TEMPLATE") {
		t.Fatalf("continuation prompt repeated role template: %q", secondPrompt)
	}
	if !strings.Contains(secondPrompt, "# Сообщение пользователя") || !strings.Contains(secondPrompt, "Follow-up task.") || !strings.Contains(secondPrompt, "Продолжай существующую сессию Codex") {
		t.Fatalf("second prompt = %q", secondPrompt)
	}
	for _, expected := range []string{
		"mattermost_list_chats(target_agent=",
		"mattermost_get_chat(chat=",
		"mattermost_start_agent_thread(target_chat=",
		"mattermost_return_to_requester(message=",
	} {
		if !strings.Contains(secondPrompt, expected) {
			t.Fatalf("continuation prompt missing runtime contract %q: %q", expected, secondPrompt)
		}
	}
}

func TestChatRunRetriesFailedCapacityTurnInSavedSession(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		PromptTemplate:    "BOOTSTRAP TEMPLATE: {{.Task.Body}}",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      testLocalizer(t, texti18n.RussianLocale),
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	first := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner-user",
		UserName:  "owner",
		Message:   "Выполни задачу.",
	})
	if first.RunID == "" || len(store.sessionTurns) != 1 {
		t.Fatalf("first result=%#v turns=%#v", first, store.sessionTurns)
	}
	sessionKey := runner.startedSessionKey
	session := store.agentSessions[sessionKey]
	session.Status = agentSessionStatusError
	session.CodexSessionID = "codex-session-1"
	store.agentSessions[sessionKey] = session
	failedTurn := store.sessionTurns[0]
	failedTurn.Status = agentSessionTurnFailed
	store.sessionTurns[0] = failedTurn

	retried, err := svc.RetryAgentTurn(context.Background(), AgentTurnRetryRequest{
		Session:  session,
		Turn:     failedTurn,
		UserID:   "owner-user",
		UserName: "owner",
	})
	if err != nil {
		t.Fatalf("RetryAgentTurn() error = %v", err)
	}
	if retried.RunID == "" || len(store.sessionTurns) != 2 {
		t.Fatalf("retry result=%#v turns=%#v", retried, store.sessionTurns)
	}
	retryPrompt := store.sessionTurns[1].Message
	if !strings.Contains(retryPrompt, "Продолжи тот же turn") || strings.Contains(retryPrompt, "BOOTSTRAP TEMPLATE") || strings.Contains(retryPrompt, "Выполни задачу") {
		t.Fatalf("retry prompt = %q", retryPrompt)
	}
}

func TestChatRunQueuesFollowUpsForRunningThreadSessionWithoutRestart(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	store.threadContexts[1] = entity.ThreadContext{
		ID:                      1,
		ProjectID:               1,
		ChatID:                  1,
		MattermostChannelID:     "channel-1",
		MattermostRootPostID:    "post-1",
		RepositoryID:            1,
		RepositoryProvider:      "github",
		RepositoryOwner:         "codex-k8s",
		RepositoryName:          "matter-codex",
		RepositoryDefaultBranch: "main",
		Status:                  threadContextStatusConfigured,
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	first := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Start the thread work.",
	})
	if first.RunID == "" {
		t.Fatalf("first result = %#v", first)
	}
	if len(runner.sessionRuns) != 1 || len(store.sessionTurns) != 1 {
		t.Fatalf("initial session state runner=%#v turns=%#v", runner.sessionRuns, store.sessionTurns)
	}
	sessionKey := agentSessionKey(1, 1, agentSessionScopeThreadRole, "post-1")
	session := store.agentSessions[sessionKey]
	session.Status = agentSessionStatusRunning
	session.ActiveTurnID = store.sessionTurns[0].ID
	session.ActiveRunID = store.sessionTurns[0].RunID
	store.agentSessions[sessionKey] = session
	store.sessionTurns[0].Status = agentSessionTurnRunning

	for _, item := range []struct {
		postID  string
		message string
	}{
		{postID: "reply-1", message: "First follow-up."},
		{postID: "reply-2", message: "Second follow-up."},
		{postID: "reply-3", message: "Third follow-up."},
	} {
		result := svc.HandleChatPost(context.Background(), ChatPostCommand{
			ChannelID:  "channel-1",
			PostID:     item.postID,
			RootPostID: "post-1",
			UserID:     "owner",
			UserName:   "owner",
			Message:    item.message,
		})
		if result.RunID == "" || result.Mode != "session" {
			t.Fatalf("follow-up %s result = %#v", item.postID, result)
		}
	}
	if len(runner.sessionRuns) != 1 {
		t.Fatalf("running session was restarted: %#v", runner.sessionRuns)
	}
	if len(store.sessionTurns) != 4 {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
	session = store.agentSessions[sessionKey]
	if session.Status != agentSessionStatusRunning || session.ActiveTurnID != store.sessionTurns[0].ID || session.ActiveRunID != store.sessionTurns[0].RunID {
		t.Fatalf("running session state was changed: %#v", session)
	}
	for index, turn := range store.sessionTurns[1:] {
		if turn.Status != agentSessionTurnQueued {
			t.Fatalf("turn %d status = %q", index+1, turn.Status)
		}
		if !strings.Contains(turn.Message, "Продолжай существующую сессию Codex") {
			t.Fatalf("turn %d is not a continuation prompt: %q", index+1, turn.Message)
		}
	}
}

func TestChatRunEnsuresIdleSessionRuntimeBeforeQueueingContinuation(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	first := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Start the work.",
	})
	if first.RunID == "" || len(runner.sessionRuns) != 1 || len(store.sessionTurns) != 1 {
		t.Fatalf("first result=%#v runner=%#v turns=%#v", first, runner.sessionRuns, store.sessionTurns)
	}
	store.sessionTurns[0].Status = agentSessionTurnSucceeded

	second := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "reply-1",
		RootPostID: "post-1",
		UserID:     "owner",
		UserName:   "owner",
		Message:    "Continue after the previous pod expired.",
	})
	if second.RunID == "" || second.Mode != "session" {
		t.Fatalf("second result = %#v", second)
	}
	if len(runner.sessionRuns) != 2 {
		t.Fatalf("idle session runtime was not ensured: %#v", runner.sessionRuns)
	}
	if len(store.sessionTurns) != 2 || store.sessionTurns[1].Status != agentSessionTurnQueued {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
	if !strings.Contains(store.sessionTurns[1].Message, "Continue after the previous pod expired.") {
		t.Fatalf("continuation prompt = %q", store.sessionTurns[1].Message)
	}
}

func TestChatRunEvictsOldestIdleSessionPodOnCapacityPressure(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	oldestActivity := time.Now().Add(-3 * time.Hour)
	newerActivity := time.Now().Add(-time.Hour)
	store.agentSessions = map[string]entity.AgentSession{
		"busy-session": {
			ID:                  3,
			SessionKey:          "busy-session",
			ProjectID:           1,
			ChatID:              1,
			RoleID:              1,
			Status:              agentSessionStatusRunning,
			ActiveTurnID:        99,
			KubernetesNamespace: "mattermost",
			PodName:             "mc-session-busy-session",
			PVCName:             "mc-session-ws-busy-session",
			TokenSecretRef:      "matter-codex-session-busy-session",
			LastActivityAt:      time.Now().Add(-4 * time.Hour),
		},
		"oldest-idle": {
			ID:                  1,
			SessionKey:          "oldest-idle",
			ProjectID:           1,
			ChatID:              1,
			RoleID:              1,
			Status:              agentSessionStatusIdle,
			KubernetesNamespace: "mattermost",
			PodName:             "mc-session-oldest-idle",
			PVCName:             "mc-session-ws-oldest-idle",
			TokenSecretRef:      "matter-codex-session-oldest-idle",
			LastActivityAt:      oldestActivity,
		},
		"newer-idle": {
			ID:                  2,
			SessionKey:          "newer-idle",
			ProjectID:           1,
			ChatID:              1,
			RoleID:              1,
			Status:              agentSessionStatusIdle,
			KubernetesNamespace: "mattermost",
			PodName:             "mc-session-newer-idle",
			PVCName:             "mc-session-ws-newer-idle",
			TokenSecretRef:      "matter-codex-session-newer-idle",
			LastActivityAt:      newerActivity,
		},
	}
	runner := &fakeRuntimeRunner{
		sessionStartErrors: []error{
			runtimerepo.NewAgentSessionCapacityError("test quota pressure", errors.New("quota exceeded")),
			nil,
		},
	}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:          testLocalizer(t, texti18n.DefaultLocale),
		Store:              store,
		RuntimeRunner:      runner,
		StorageReady:       true,
		RuntimeReady:       true,
		DisableMonitor:     true,
		CapacityRetryDelay: time.Nanosecond,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-capacity",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Start after reclaiming idle capacity.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.sessionRuns) != 2 {
		t.Fatalf("session starts = %#v", runner.sessionRuns)
	}
	if len(runner.cleanedSessionKeys) != 1 || runner.cleanedSessionKeys[0] != "oldest-idle" {
		t.Fatalf("cleaned sessions = %#v", runner.cleanedSessionKeys)
	}
	oldest := store.agentSessions["oldest-idle"]
	if oldest.PodName != "" || oldest.KubernetesNamespace != "" {
		t.Fatalf("oldest pod binding was not cleared: %#v", oldest)
	}
	if oldest.PVCName != "mc-session-ws-oldest-idle" || oldest.TokenSecretRef != "matter-codex-session-oldest-idle" {
		t.Fatalf("oldest resumable state was not preserved: %#v", oldest)
	}
	if newer := store.agentSessions["newer-idle"]; newer.PodName != "mc-session-newer-idle" {
		t.Fatalf("newer idle pod was evicted: %#v", newer)
	}
	if busy := store.agentSessions["busy-session"]; busy.PodName != "mc-session-busy-session" {
		t.Fatalf("active session pod was evicted: %#v", busy)
	}
	if len(store.sessionTurns) != 1 || store.sessionTurns[0].Status != agentSessionTurnQueued {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
}

func TestChatRunKeepsTurnQueuedWhenCapacityCannotBeReclaimed(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{
		sessionStartErrors: []error{
			runtimerepo.NewAgentSessionCapacityError("test scheduler pressure", errors.New("insufficient memory")),
		},
	}
	publisher := &fakeThreadPublisher{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:          testLocalizer(t, texti18n.DefaultLocale),
		Store:              store,
		RuntimeRunner:      runner,
		ThreadPublisher:    publisher,
		StorageReady:       true,
		RuntimeReady:       true,
		DisableMonitor:     true,
		CapacityRetryDelay: time.Nanosecond,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-capacity-wait",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Wait for runtime capacity.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.cleanedSessionKeys) != 0 {
		t.Fatalf("unexpected cleanup = %#v", runner.cleanedSessionKeys)
	}
	if len(store.sessionTurns) != 1 || store.sessionTurns[0].Status != agentSessionTurnQueued {
		t.Fatalf("queued turn was not retained: %#v", store.sessionTurns)
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, result.RunID) {
		t.Fatalf("capacity wait notification = %#v", publisher.posts)
	}
}

func TestChatRunRepairEnsuresQueuedIdleSessionRuntime(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	sessionKey := agentSessionKey(1, 1, agentSessionScopeThreadRole, "post-1")
	store.agentSessions = map[string]entity.AgentSession{
		sessionKey: {
			ID:                   1,
			SessionKey:           sessionKey,
			ProjectID:            1,
			ChatID:               1,
			RoleID:               1,
			SessionScope:         agentSessionScopeThreadRole,
			MattermostChannelID:  "channel-1",
			MattermostRootPostID: "post-1",
			Status:               agentSessionStatusIdle,
			PodName:              "mc-session-old",
			PVCName:              "mc-session-ws-old",
			TokenSecretRef:       "session-secret",
			TTLSeconds:           defaultThreadSessionTTLSeconds,
		},
	}
	store.sessionTurns = []entity.AgentSessionTurn{
		{ID: 1, SessionID: 1, RunID: "run-1", Status: agentSessionTurnQueued, MattermostChannelID: "channel-1", MattermostRootPostID: "post-1"},
	}
	runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"session-secret": "session-token"}}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      testLocalizer(t, texti18n.DefaultLocale),
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result, err := svc.RepairAgentSessions(context.Background(), 10)
	if err != nil {
		t.Fatalf("RepairAgentSessions() error = %v", err)
	}
	if result.QueuedSessionsEnsured != 1 || result.StaleSessionsReset != 0 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.sessionRuns) != 1 || runner.sessionRuns[0].SessionKey != sessionKey {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	session := store.agentSessions[sessionKey]
	if session.PodName != "mc-session-"+sessionKey || session.TokenSecretRef == "" {
		t.Fatalf("session = %#v", session)
	}
}

func TestChatRunRepairResetsStaleActiveSession(t *testing.T) {
	store := chatRuntimeStore()
	sessionKey := agentSessionKey(1, 1, agentSessionScopeThreadRole, "post-1")
	store.agentSessions = map[string]entity.AgentSession{
		sessionKey: {
			ID:             1,
			SessionKey:     sessionKey,
			ProjectID:      1,
			ChatID:         1,
			RoleID:         1,
			Status:         agentSessionStatusRunning,
			ActiveTurnID:   1,
			ActiveRunID:    "run-1",
			PodName:        "mc-session-stale",
			PVCName:        "mc-session-ws-stale",
			TokenSecretRef: "session-secret",
			TTLSeconds:     defaultThreadSessionTTLSeconds,
		},
	}
	store.sessionTurns = []entity.AgentSessionTurn{
		{ID: 1, SessionID: 1, RunID: "run-1", Status: agentSessionTurnSucceeded},
	}
	runner := &fakeRuntimeRunner{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      testLocalizer(t, texti18n.DefaultLocale),
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result, err := svc.RepairAgentSessions(context.Background(), 10)
	if err != nil {
		t.Fatalf("RepairAgentSessions() error = %v", err)
	}
	if result.StaleSessionsReset != 1 || result.QueuedSessionsEnsured != 0 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
	session := store.agentSessions[sessionKey]
	if session.Status != agentSessionStatusIdle || session.ActiveTurnID != 0 || session.PodName != "" || runner.cleanedSessionKey != sessionKey {
		t.Fatalf("session=%#v runner=%#v", session, runner)
	}
}

func TestChatRunRepairResetsTerminalRunningSessionAndEnsuresQueue(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "reviewer",
		RoleType:          "reviewer",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Review", ChatType: "worker_reviewer"}
	sessionKey := agentSessionKey(1, 1, agentSessionScopeThreadRole, "post-1")
	store.agentSessions = map[string]entity.AgentSession{
		sessionKey: {
			ID:                   1,
			SessionKey:           sessionKey,
			ProjectID:            1,
			ChatID:               1,
			RoleID:               1,
			SessionScope:         agentSessionScopeThreadRole,
			MattermostChannelID:  "channel-1",
			MattermostRootPostID: "post-1",
			Status:               agentSessionStatusRunning,
			ActiveTurnID:         1,
			ActiveRunID:          "run-1",
			PodName:              "mc-session-" + sessionKey,
			PVCName:              "mc-session-ws-" + sessionKey,
			TokenSecretRef:       "session-secret",
			TTLSeconds:           defaultThreadSessionTTLSeconds,
		},
	}
	store.sessionTurns = []entity.AgentSessionTurn{
		{ID: 1, SessionID: 1, RunID: "run-1", Status: agentSessionTurnRunning, MattermostChannelID: "channel-1", MattermostRootPostID: "post-1"},
		{ID: 2, SessionID: 1, RunID: "run-2", Status: agentSessionTurnQueued, MattermostChannelID: "channel-1", MattermostRootPostID: "post-1"},
	}
	runner := &fakeRuntimeRunner{
		botTokenSecrets: map[string]string{"session-secret": "session-token"},
		sessionRuntimeHealth: runtimerepo.AgentSessionRuntimeHealth{
			SessionKey: sessionKey,
			Exists:     true,
			Phase:      "Failed",
			Terminal:   true,
			Reason:     "container agent-runner terminated: OOMKilled exit=137",
		},
	}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      testLocalizer(t, texti18n.DefaultLocale),
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result, err := svc.RepairAgentSessions(context.Background(), 10)
	if err != nil {
		t.Fatalf("RepairAgentSessions() error = %v", err)
	}
	if result.StaleSessionsReset != 1 || result.QueuedSessionsEnsured != 1 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
	if store.sessionTurns[0].Status != agentSessionTurnFailed || !strings.Contains(store.sessionTurns[0].ErrorMessage, "OOMKilled") {
		t.Fatalf("running turn was not failed with OOM reason: %#v", store.sessionTurns[0])
	}
	if store.sessionTurns[1].Status != agentSessionTurnQueued {
		t.Fatalf("queued turn changed unexpectedly: %#v", store.sessionTurns[1])
	}
	if runner.cleanedSessionKey != sessionKey {
		t.Fatalf("cleanedSessionKey = %q", runner.cleanedSessionKey)
	}
	if len(runner.sessionRuns) != 1 || runner.sessionRuns[0].SessionKey != sessionKey {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	session := store.agentSessions[sessionKey]
	if session.Status != agentSessionStatusIdle || session.ActiveTurnID != 0 || session.PodName != "mc-session-"+sessionKey {
		t.Fatalf("session = %#v", session)
	}
}

func TestChatRunDoesNotCreateSessionWhenFirstRoleTemplateFails(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		PromptTemplate:    "{{.Missing.Field}}",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "First task.",
	})

	if result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if len(store.sessionTurns) != 0 || len(store.agentSessions) != 0 || runner.startedSessionKey != "" {
		t.Fatalf("session should not be created, sessions=%#v turns=%#v runner=%#v", store.agentSessions, store.sessionTurns, runner.startedSessionKey)
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, "render role prompt template") {
		t.Fatalf("posts = %#v", publisher.posts)
	}
}

func TestChatRunPostsOpenAIReauthInThreadWhenAuthSecretIsInvalid(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{authSecretNotReady: true}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Continue the work.",
	})

	if result.RunID != "" || result.Mode != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.authSecretChecks == 0 {
		t.Fatal("expected auth secret preflight check")
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 {
		t.Fatalf("agent session should not start, runner=%#v turns=%#v", runner.sessionRuns, store.sessionTurns)
	}
	if len(publisher.posts) != 1 || publisher.posts[0].RootPostID != "post-1" {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	message := publisher.posts[0].Message
	if !strings.Contains(message, "ABCD-12345") || !strings.Contains(message, "https://auth.openai.com/codex/device") {
		t.Fatalf("reauth message = %q", message)
	}
}

func TestChatRunDoesNotStartReauthWhenAuthCheckInfrastructureFails(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{authSecretCheckErr: errors.New("auth check pod startup timed out")}
	publisher := &fakeThreadPublisher{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Continue the work.",
	})

	if result.RunID != "" || runner.authAccount != "" || runner.startedSessionKey != "" {
		t.Fatalf("unexpected run or reauth: result=%#v runner=%#v", result, runner)
	}
	if len(store.sessionTurns) != 0 {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, "check codex auth secret") {
		t.Fatalf("posts = %#v", publisher.posts)
	}
}

func TestChatRunDoesNotPostEmptyDeviceCodeWhenReauthJobIsNotReady(t *testing.T) {
	originalWait := codexAuthDeviceCodeWait
	codexAuthDeviceCodeWait = time.Millisecond
	t.Cleanup(func() {
		codexAuthDeviceCodeWait = originalWait
	})

	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{authSecretNotReady: true, authStatusWithoutDeviceCode: true}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Continue the work.",
	})

	if result.RunID != "" || result.Mode != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 {
		t.Fatalf("agent session should not start, runner=%#v turns=%#v", runner.sessionRuns, store.sessionTurns)
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	message := publisher.posts[0].Message
	if strings.Contains(message, "code: ``") || strings.Contains(message, "open: ") {
		t.Fatalf("message exposes empty device-code fields: %q", message)
	}
	if !strings.Contains(message, "did not provide url/code") {
		t.Fatalf("start failure message = %q", message)
	}
}

func TestChatRunStartsDeveloperModeForWorkerRoleWithRepository(t *testing.T) {
	store := chatRuntimeStore()
	store.repositories[repositoryStoreKey("github", "codex-k8s", "matter-codex")] = entity.Repository{
		ID:                1,
		Provider:          "github",
		Owner:             "codex-k8s",
		Name:              "matter-codex",
		DefaultBranch:     "main",
		GitHubAccountName: "agent",
		Status:            "active",
	}
	store.projectRepositories["1:1"] = entity.ProjectRepository{
		ID:            1,
		ProjectID:     1,
		RepositoryID:  1,
		Provider:      "github",
		Owner:         "codex-k8s",
		Name:          "matter-codex",
		DefaultBranch: "main",
		IsDefault:     true,
	}
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "worker",
		RoleType:          "worker",
		OpenAIAccountName: "main",
		GitHubAccountName: "agent",
		SandboxMode:       "danger-full-access",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Dev", ChatType: "worker_reviewer"}
	store.setChatBindings(1, []int64{1}, []int64{1})
	store.threadContexts[1] = entity.ThreadContext{
		ID:                      1,
		ProjectID:               1,
		ChatID:                  1,
		MattermostChannelID:     "channel-1",
		MattermostRootPostID:    "post-1",
		RepositoryID:            1,
		RepositoryProvider:      "github",
		RepositoryOwner:         "codex-k8s",
		RepositoryName:          "matter-codex",
		RepositoryDefaultBranch: "main",
		Status:                  threadContextStatusConfigured,
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		Message:   "Update README with a short note.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey == "" || runner.sessionGitHubSecret != "matter-codex-github-agent" {
		t.Fatalf("session runner = %#v", runner.sessionRuns)
	}
	if runner.sessionRuns[0].RepositoryOwner != "codex-k8s" || runner.sessionRuns[0].RepositoryName != "matter-codex" {
		t.Fatalf("session repository = %#v", runner.sessionRuns[0])
	}
}

func TestChatRunPromptsThreadRepositoryChoiceAndRunsWithoutRepository(t *testing.T) {
	store := chatRuntimeStore()
	project := store.projects[1]
	project.GitHubOwner = "codex-k8s"
	project.GitHubAccountName = "agent"
	store.projects[1] = project
	store.repositories[repositoryStoreKey("github", "codex-k8s", "matter-codex")] = entity.Repository{
		ID:                1,
		Provider:          "github",
		Owner:             "codex-k8s",
		Name:              "matter-codex",
		DefaultBranch:     "main",
		GitHubAccountName: "agent",
		Status:            "active",
	}
	store.projectRepositories["1:1"] = entity.ProjectRepository{
		ID:            1,
		ProjectID:     1,
		RepositoryID:  1,
		Provider:      "github",
		Owner:         "codex-k8s",
		Name:          "matter-codex",
		DefaultBranch: "main",
		IsDefault:     true,
	}
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "worker",
		RoleType:          "worker",
		OpenAIAccountName: "main",
		GitHubAccountName: "agent",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Dev", ChatType: "worker_reviewer"}
	store.setChatBindings(1, []int64{1}, []int64{1})
	runner := &fakeRuntimeRunner{}
	publisher := &fakeThreadPublisher{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       localizer,
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://matter-codex.example/mattermost/actions/agents",
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Start from a blank workspace.",
	})

	if !result.Ignored && result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.cards) != 1 || len(publisher.cards[0].Actions) < 2 {
		t.Fatalf("cards = %#v", publisher.cards)
	}
	if runner.startedSessionKey != "" {
		t.Fatalf("session should not start before repository choice: %#v", runner.sessionRuns)
	}
	threadContext, err := store.GetThreadContext(context.Background(), 1, "post-1")
	if err != nil || threadContext.Status != threadContextStatusPending {
		t.Fatalf("thread context = %#v err=%v", threadContext, err)
	}

	selected, err := svc.SelectThreadRepository(context.Background(), ThreadRepositorySelectionInput{ThreadContextID: threadContext.ID, RepositoryID: 0})
	if err != nil {
		t.Fatalf("select thread repository: %v", err)
	}
	if selected.RunID == "" || runner.startedSessionKey == "" {
		t.Fatalf("selection result = %#v runner=%#v", selected, runner.sessionRuns)
	}
	if runner.sessionRuns[0].RepositoryOwner != "" || runner.sessionRuns[0].RepositoryName != "" {
		t.Fatalf("session should start without repository checkout: %#v", runner.sessionRuns[0])
	}
}

func TestChatRunRetriesConfiguredThreadRepositorySelectionWhenSessionWasNotCreated(t *testing.T) {
	store := chatRuntimeStore()
	project := store.projects[1]
	project.GitHubOwner = "codex-k8s"
	project.GitHubAccountName = "agent"
	store.projects[1] = project
	store.repositories[repositoryStoreKey("github", "codex-k8s", "matter-codex")] = entity.Repository{
		ID:                1,
		Provider:          "github",
		Owner:             "codex-k8s",
		Name:              "matter-codex",
		DefaultBranch:     "main",
		GitHubAccountName: "agent",
		Status:            "active",
	}
	store.projectRepositories["1:1"] = entity.ProjectRepository{
		ID:            1,
		ProjectID:     1,
		RepositoryID:  1,
		Provider:      "github",
		Owner:         "codex-k8s",
		Name:          "matter-codex",
		DefaultBranch: "main",
		IsDefault:     true,
	}
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "manager",
		RoleType:          "manager",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.agentRoles[2] = entity.AgentRole{
		ID:                2,
		ProjectID:         1,
		Name:              "reviewer",
		RoleType:          "reviewer",
		OpenAIAccountName: "main",
		GitHubAccountName: "agent",
		Enabled:           true,
	}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "reviewer", MattermostUserID: "reviewer-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Review", ChatType: "worker_reviewer"}
	store.setChatBindings(1, []int64{1, 2}, []int64{1})
	store.threadContexts[1] = entity.ThreadContext{
		ID:                      1,
		ProjectID:               1,
		ChatID:                  1,
		MattermostChannelID:     "channel-1",
		MattermostRootPostID:    "post-1",
		RepositoryID:            1,
		RepositoryProvider:      "github",
		RepositoryOwner:         "codex-k8s",
		RepositoryName:          "matter-codex",
		RepositoryDefaultBranch: "main",
		Status:                  threadContextStatusConfigured,
		PendingMattermostPostID: "post-1",
		PendingUserID:           "owner",
		PendingUserName:         "owner",
		PendingMessage:          "@reviewer review https://github.com/codex-k8s/matter-codex/pull/37",
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	selected, err := svc.SelectThreadRepository(context.Background(), ThreadRepositorySelectionInput{ThreadContextID: 1, RepositoryID: 1})
	if err != nil {
		t.Fatalf("SelectThreadRepository() error = %v", err)
	}
	if selected.RunID == "" {
		t.Fatalf("selection did not replay pending message: %#v", selected)
	}
	if len(runner.sessionRuns) != 1 || runner.sessionRuns[0].Role != "reviewer" {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	if len(store.sessionTurns) != 1 || store.sessionTurns[0].MattermostRootPostID != "post-1" {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
}

func TestChatRunIgnoresAgentBotMessageWithoutAgentMention(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "manager-user",
		UserName:   "manager",
		Message:    "@owner fyi: task complete",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("bot message should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunIgnoresNoTriggerHumanMention(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner-user",
		UserName:  "owner",
		Message:   "#notrigger @sre this is a repost, do not run",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("no-trigger human message should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunIgnoresNoTriggerAgentBotMention(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "manager-user",
		UserName:   "manager",
		Message:    "@sre #silent reposted message, do not run",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("no-trigger bot message should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunIgnoresMatterCodexSystemPost(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      testLocalizer(t, texti18n.DefaultLocale),
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "system-audit-1",
		RootPostID: "post-1",
		UserID:     "owner-user",
		UserName:   "owner",
		Message:    "matter-codex: @manager запустил @sre с prompt:\n\n```markdown\n@sre deploy\n```",
		Props:      map[string]any{"matter_codex_event": "agent_request"},
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("system audit post should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunIgnoresAgentBotMentionAtMessageStart(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "manager-user",
		UserName:   "manager",
		Message:    "@sre please check deployment",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.sessionRuns) != 0 {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
}

func TestChatRunIgnoresAgentBotMentionOutsideLeadingLaunch(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "manager-user",
		UserName:   "manager",
		Message:    "Status update:\n- SRE follow-up: @sre may need to check deployment after merge.",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("status mention should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunRoutesHumanMentionOutsideLeadingLaunch(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner-user",
		UserName:  "owner",
		Message:   "After this plan, please ask @sre to check deployment.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.sessionRuns) != 1 || runner.sessionRuns[0].Role != "sre" {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	if runner.startedSessionKey != agentSessionKey(1, 2, agentSessionScopeThreadRole, "post-1") {
		t.Fatalf("session key = %q", runner.startedSessionKey)
	}
}

func TestChatRunRoutesHumanMentionToProjectRoleOutsideChatParticipants(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "docs", RoleType: "writer", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "docs", MattermostUserID: "docs-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Control", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      testLocalizer(t, texti18n.DefaultLocale),
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "post-docs",
		RootPostID: "root-1",
		UserID:     "owner-user",
		UserName:   "owner",
		Message:    "@docs continue the existing work",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.sessionRuns) != 1 || runner.sessionRuns[0].Role != "docs" {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	if runner.startedSessionKey != agentSessionKey(1, 2, agentSessionScopeThreadRole, "root-1") {
		t.Fatalf("session key = %q", runner.startedSessionKey)
	}
}

func TestChatRunIgnoresAgentBotMentionInMarkdownCode(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "manager-user",
		UserName:   "manager",
		Message:    "Status: `@sre` has no cluster-wide RBAC.\n```text\n@manager and @sre are role names here\n```",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("markdown code mention should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunIgnoresAgentBotSelfMention(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "manager-user",
		UserName:   "manager",
		Message:    "@manager continue the same work",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("self mention should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunIgnoresAgentBotSelfMentionWithSharedBotUserAcrossProjects(t *testing.T) {
	store := chatRuntimeStore()
	store.projects[2] = entity.Project{ID: 2, Name: "Other", Slug: "other"}
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "developer", RoleType: "worker", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[99] = entity.AgentRole{ID: 99, ProjectID: 2, Name: "developer", RoleType: "worker", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1:  {ID: 1, ProjectID: 1, RoleID: 1, Username: "developer", MattermostUserID: "shared-developer-user", Status: "configured"},
		99: {ID: 99, ProjectID: 2, RoleID: 99, Username: "developer", MattermostUserID: "shared-developer-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Dev", ChatType: "single_custom_role"}
	store.setChatBindings(1, []int64{1}, nil)
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "bot-reply-1",
		RootPostID: "post-1",
		UserID:     "shared-developer-user",
		UserName:   "developer",
		Message:    "@developer continue only if a human asks",
	})

	if !result.Ignored || result.RunID != "" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey != "" || len(store.sessionTurns) != 0 || len(store.threadContexts) != 0 {
		t.Fatalf("cross-project shared bot user self mention should not start runtime or create thread context, runner=%#v turns=%#v contexts=%#v", runner.sessionRuns, store.sessionTurns, store.threadContexts)
	}
}

func TestChatRunFallsBackToProjectGitHubAccount(t *testing.T) {
	store := chatRuntimeStore()
	project := store.projects[1]
	project.GitHubAccountName = "project-gh"
	store.projects[1] = project
	store.githubAccounts["project-gh"] = entity.GitHubAccount{Name: "project-gh", SecretRef: "matter-codex-github-project", Status: "configured", Username: "project-agent", Email: "project@example.invalid"}
	store.repositories[repositoryStoreKey("github", "codex-k8s", "kodex")] = entity.Repository{
		ID:                1,
		Provider:          "github",
		Owner:             "codex-k8s",
		Name:              "kodex",
		DefaultBranch:     "main",
		GitHubAccountName: "legacy-repo-account",
		Status:            "active",
	}
	store.projectRepositories["1:1"] = entity.ProjectRepository{
		ID:            1,
		ProjectID:     1,
		RepositoryID:  1,
		Provider:      "github",
		Owner:         "codex-k8s",
		Name:          "kodex",
		DefaultBranch: "main",
		IsDefault:     true,
	}
	store.agentRoles[1] = entity.AgentRole{
		ID:                1,
		ProjectID:         1,
		Name:              "worker",
		RoleType:          "worker",
		OpenAIAccountName: "main",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Dev", ChatType: "worker_reviewer"}
	store.setChatBindings(1, []int64{1}, []int64{1})
	store.threadContexts[1] = entity.ThreadContext{
		ID:                      1,
		ProjectID:               1,
		ChatID:                  1,
		MattermostChannelID:     "channel-1",
		MattermostRootPostID:    "post-1",
		RepositoryID:            1,
		RepositoryProvider:      "github",
		RepositoryOwner:         "codex-k8s",
		RepositoryName:          "kodex",
		RepositoryDefaultBranch: "main",
		Status:                  threadContextStatusConfigured,
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:      localizer,
		Store:          store,
		RuntimeRunner:  runner,
		StorageReady:   true,
		RuntimeReady:   true,
		DisableMonitor: true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-1",
		UserID:    "owner",
		Message:   "Work through the project repository.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedSessionKey == "" || runner.sessionGitHubSecret != "matter-codex-github-project" {
		t.Fatalf("session runner = %#v", runner.sessionRuns)
	}
}

func chatRuntimeStore() *fakeAdminStore {
	return &fakeAdminStore{
		repositories: map[string]entity.Repository{},
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Platform", Slug: "platform"},
		},
		projectRepositories: map[string]entity.ProjectRepository{},
		agentRoles:          map[int64]entity.AgentRole{},
		chats:               map[int64]entity.Chat{},
		chatParticipants:    map[int64][]entity.ChatParticipant{},
		chatRepositories:    map[int64][]entity.ChatRepositoryBinding{},
		threadContexts:      map[int64]entity.ThreadContext{},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"main": {Name: "main", SecretRef: "matter-codex-codex-auth-main", Status: "authorized"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured", Username: "agent", Email: "agent@example.invalid"},
		},
	}
}

type fakeThreadPublisher struct {
	posts                []MattermostThreadPostInput
	updates              []MattermostThreadUpdateInput
	cards                []MattermostCard
	cardUpdates          []MattermostCard
	reactions            []MattermostPostReactionInput
	reactionTokens       []string
	postWithTokenErr     error
	updateWithTokenErr   error
	postWithTokenCalls   int
	updateWithTokenCalls int
}

func (publisher *fakeThreadPublisher) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.posts = append(publisher.posts, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "reply-" + input.RootPostID}, nil
}

func (publisher *fakeThreadPublisher) PostThreadMessageWithToken(_ context.Context, _ string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.postWithTokenCalls++
	if publisher.postWithTokenErr != nil {
		return MattermostPostRef{}, publisher.postWithTokenErr
	}
	return publisher.PostThreadMessage(context.Background(), input)
}

func (publisher *fakeThreadPublisher) UpdateThreadMessage(_ context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	publisher.updates = append(publisher.updates, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: input.PostID}, nil
}

func (publisher *fakeThreadPublisher) UpdateThreadMessageWithToken(_ context.Context, _ string, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	publisher.updateWithTokenCalls++
	if publisher.updateWithTokenErr != nil {
		return MattermostPostRef{}, publisher.updateWithTokenErr
	}
	return publisher.UpdateThreadMessage(context.Background(), input)
}

func (publisher *fakeThreadPublisher) PostThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	publisher.cards = append(publisher.cards, card)
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: "card-" + card.RootPostID}, nil
}

func (publisher *fakeThreadPublisher) UpdateThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	publisher.cardUpdates = append(publisher.cardUpdates, card)
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: card.PostID}, nil
}

func (publisher *fakeThreadPublisher) AddPostReactionWithToken(_ context.Context, token string, input MattermostPostReactionInput) error {
	publisher.reactionTokens = append(publisher.reactionTokens, token)
	publisher.reactions = append(publisher.reactions, input)
	return nil
}

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
