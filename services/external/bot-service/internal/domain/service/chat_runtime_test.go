package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	kubernetesruntime "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/kubernetes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

func TestChatRunRejectsClosedHistoricalThread(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	store.threadContexts[1] = entity.ThreadContext{
		ID:                   1,
		ProjectID:            1,
		ChatID:               1,
		MattermostChannelID:  "channel-1",
		MattermostRootPostID: "old-root",
		Status:               threadContextStatusClosed,
	}
	publisher := &fakeThreadPublisher{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   &fakeRuntimeRunner{},
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "reply-1",
		RootPostID: "old-root",
		UserID:     "owner",
		UserName:   "owner",
		Message:    "Continue old work",
	})
	if result.RunID != "" || len(store.sessionTurns) != 0 {
		t.Fatalf("result=%#v turns=%#v", result, store.sessionTurns)
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, "closed for agent runs") {
		t.Fatalf("posts = %#v", publisher.posts)
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
	if len(publisher.posts) != 0 || len(publisher.cards) != 1 || publisher.cards[0].Props["status"] != agentSessionTurnQueued {
		t.Fatalf("chat handler must create one queued status card, posts=%#v cards=%#v", publisher.posts, publisher.cards)
	}
	if len(store.sessionTurns) != 1 || !strings.Contains(store.sessionTurns[0].Message, "Help me decompose the task.") || !strings.Contains(store.sessionTurns[0].Message, "Проект: Platform") {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
}

func TestExistingSessionKeepsOpenAIAccountAfterRoleBindingChanges(t *testing.T) {
	base := chatRuntimeStore()
	store := &runtimeRevisionFakeStore{
		fakeAdminStore: base, revisions: map[string]entity.RuntimeRevision{},
		states: map[string]entity.AgentSessionRuntimeRevisionState{}, archives: map[int64][]entity.AgentSessionArchive{},
	}
	store.openAIAccounts["secondary"] = entity.OpenAIAccount{
		Name: "secondary", SecretRef: "matter-codex-codex-auth-secondary", Status: "authorized",
	}
	role := entity.AgentRole{
		ID: 1, ProjectID: 1, Name: "developer", RoleType: "worker",
		OpenAIAccountName: "main", KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: true,
	}
	store.agentRoles[role.ID] = role
	chat := entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-account-affinity", Slug: "account-affinity", Name: "Account affinity"}
	store.chats[chat.ID] = chat
	runner := &restoringRuntimeRunner{fakeRuntimeRunner: &fakeRuntimeRunner{}}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer: testLocalizer(t, texti18n.DefaultLocale), Store: store, RuntimeRunner: runner,
		StorageReady: true, RuntimeReady: true, DisableMonitor: true,
		BotServiceURL: "http://bot-service", AgentRunnerImage: "agent-runner@sha256:synthetic",
	})
	request := AgentTurnRequest{
		Project: store.projects[1], Chat: chat, Role: role, UserID: "owner", UserName: "owner",
		UserMessage: "first turn", PreparedPrompt: "first prepared prompt", SourcePostID: "account-post-1",
		ReplyRootID: "account-root", SessionRootID: "account-root", SessionScope: agentSessionScopeThreadRole,
	}
	first, err := svc.EnqueueAgentTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("first EnqueueAgentTurn() error = %v", err)
	}
	if first.SessionKey == "" || len(runner.sessionRuns) != 1 || runner.sessionRuns[0].OpenAIAccountAlias != "main" || runner.sessionRuns[0].CodexAuthSecretName != "matter-codex-codex-auth-main" {
		t.Fatalf("first account binding: result=%#v runs=%#v", first, runner.sessionRuns)
	}

	role.OpenAIAccountName = "secondary"
	store.agentRoles[role.ID] = role
	request.Role = role
	request.UserMessage = "continuation after role account change"
	request.PreparedPrompt = "continuation prepared prompt"
	request.SourcePostID = "account-post-2"
	second, err := svc.EnqueueAgentTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("continuation EnqueueAgentTurn() error = %v", err)
	}
	if second.SessionKey != first.SessionKey || len(runner.sessionRuns) != 2 {
		t.Fatalf("continuation session: first=%#v second=%#v runs=%#v", first, second, runner.sessionRuns)
	}
	continuation := runner.sessionRuns[1]
	if continuation.OpenAIAccountAlias != "main" || continuation.CodexAuthSecretName != "matter-codex-codex-auth-main" {
		t.Fatalf("existing session followed mutable role account: %#v", continuation)
	}
	if persisted := store.agentSessions[first.SessionKey]; persisted.OpenAIAccountName != "main" {
		t.Fatalf("persisted session account affinity = %q", persisted.OpenAIAccountName)
	}
}

func TestRuntimeSecretRevisionIsStableAndRotatesForCurrentEnqueue(t *testing.T) {
	base := chatRuntimeStore()
	account := base.openAIAccounts["main"]
	account.ID = 7
	account.CredentialID = 11
	account.UpdatedAt = time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	base.openAIAccounts["main"] = account
	role := entity.AgentRole{
		ID: 1, ProjectID: 1, Name: "developer", RoleType: "worker", OpenAIAccountName: "main",
		KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: true,
	}
	base.agentRoles[role.ID] = role
	chat := entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-secret-revision", Slug: "secret-revision", Name: "Secret revision"}
	base.chats[chat.ID] = chat
	store := &runtimeRevisionFakeStore{
		fakeAdminStore: base, revisions: map[string]entity.RuntimeRevision{},
		states: map[string]entity.AgentSessionRuntimeRevisionState{}, archives: map[int64][]entity.AgentSessionArchive{},
	}
	runner := &restoringRuntimeRunner{fakeRuntimeRunner: &fakeRuntimeRunner{secretIntegrity: map[string]runtimerepo.SecretIntegrity{
		"matter-codex-codex-auth-main/auth.json": {
			SecretName: "matter-codex-codex-auth-main", SecretKey: "auth.json",
			ContentSHA256: "synthetic-content-r1", UID: "synthetic-secret-uid", ResourceVersion: "1",
		},
	}}}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer: testLocalizer(t, texti18n.DefaultLocale), Store: store, RuntimeRunner: runner,
		StorageReady: true, RuntimeReady: true, DisableMonitor: true,
		BotServiceURL: "http://bot-service", AgentRunnerImage: "agent-runner@sha256:synthetic",
	})
	request := AgentTurnRequest{
		Project: base.projects[1], Chat: chat, Role: role, UserID: "owner", UserName: "owner",
		UserMessage: "first", PreparedPrompt: "first prompt", SourcePostID: "secret-post-1",
		ReplyRootID: "secret-root", SessionRootID: "secret-root", SessionScope: agentSessionScopeThreadRole,
	}
	if _, err := svc.EnqueueAgentTurn(context.Background(), request); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	firstRevisionID := base.sessionTurns[0].RuntimeRevisionID
	firstRevision, err := store.GetRuntimeRevision(context.Background(), firstRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRevision.AuthorizationRevision != "openai:7:auth.json:r1" {
		t.Fatalf("first authorization revision = %q", firstRevision.AuthorizationRevision)
	}

	account.CredentialID = 12
	account.UpdatedAt = account.UpdatedAt.Add(time.Hour)
	base.openAIAccounts["main"] = account
	request.UserMessage = "unchanged ready check"
	request.PreparedPrompt = "second prompt"
	request.SourcePostID = "secret-post-2"
	if _, err := svc.EnqueueAgentTurn(context.Background(), request); err != nil {
		t.Fatalf("stable enqueue: %v", err)
	}
	if len(store.revisions) != 1 || base.sessionTurns[1].RuntimeRevisionID != firstRevisionID || store.secretRevisions["openai:7:auth.json"].Revision != 1 {
		t.Fatalf("unchanged Secret changed revision: revisions=%#v bindings=%#v turns=%#v", store.revisions, store.secretRevisions, base.sessionTurns)
	}

	runner.secretIntegrity["matter-codex-codex-auth-main/auth.json"] = runtimerepo.SecretIntegrity{
		SecretName: "matter-codex-codex-auth-main", SecretKey: "auth.json",
		ContentSHA256: "synthetic-content-r2", UID: "synthetic-secret-uid", ResourceVersion: "2",
	}
	request.UserMessage = "same ref after reauth"
	request.PreparedPrompt = "third prompt"
	request.SourcePostID = "secret-post-3"
	if _, err := svc.EnqueueAgentTurn(context.Background(), request); err != nil {
		t.Fatalf("rotated enqueue: %v", err)
	}
	thirdRevisionID := base.sessionTurns[2].RuntimeRevisionID
	thirdRevision, err := store.GetRuntimeRevision(context.Background(), thirdRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.revisions) != 2 || thirdRevisionID == firstRevisionID || thirdRevision.AuthorizationRevision != "openai:7:auth.json:r2" || store.secretRevisions["openai:7:auth.json"].Revision != 2 {
		t.Fatalf("reauth did not affect current enqueue: revisions=%#v bindings=%#v turns=%#v", store.revisions, store.secretRevisions, base.sessionTurns)
	}
	if strings.Contains(firstRevision.Manifest+thirdRevision.Manifest, "synthetic-content-r") {
		t.Fatal("runtime manifest contains Secret integrity value")
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

func TestChatRunPostsSingleQueuedTurnCardWithStop(t *testing.T) {
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
	if len(publisher.cards) != 1 || len(publisher.posts) != 0 {
		t.Fatalf("publisher cards=%#v posts=%#v", publisher.cards, publisher.posts)
	}
	card := publisher.cards[0]
	if card.Props["status"] != agentSessionTurnQueued || len(card.Actions) != 1 || card.Actions[0].ID != "stopturn" {
		t.Fatalf("queued status card=%#v", card)
	}
	if len(store.sessionTurns) != 1 {
		t.Fatalf("turns = %#v", store.sessionTurns)
	}
	if store.sessionTurns[0].MattermostStatusPostID == "" {
		t.Fatalf("queued turn status post was not persisted: %#v", store.sessionTurns[0])
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

func TestChatRunDoesNotEvictOrRetryWhenSessionPVCQuotaRejected(t *testing.T) {
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
	store.agentSessions = map[string]entity.AgentSession{
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
			LastActivityAt:      time.Now().Add(-3 * time.Hour),
		},
	}
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc-session-oldest-idle", Namespace: "mattermost", UID: types.UID("oldest-idle-pod-uid"), ResourceVersion: "1",
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})
	pvcCreateAttempts := 0
	client.PrependReactor("create", "persistentvolumeclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		pvcCreateAttempts++
		pvc := action.(k8stesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim)
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Resource: "persistentvolumeclaims"},
			pvc.Name,
			fmt.Errorf("exceeded quota: matter-codex-runtime-quota, requested: requests.storage=1Gi"),
		)
	})
	runner, err := kubernetesruntime.NewRunnerWithClient(client, kubernetesruntime.Config{
		Namespace:                 "mattermost",
		AgentRunnerImage:          "matter-codex-agent-runner:test",
		WorkspaceStorageSize:      "1Gi",
		AgentRunnerServiceAccount: "matter-codex-agent-runner",
	})
	if err != nil {
		t.Fatalf("NewRunnerWithClient() error = %v", err)
	}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:          testLocalizer(t, texti18n.DefaultLocale),
		Store:              store,
		RuntimeRunner:      codexReadyRuntimeRunner{Runner: runner},
		BotServiceURL:      "http://bot-service",
		StorageReady:       true,
		RuntimeReady:       true,
		DisableMonitor:     true,
		CapacityRetryDelay: time.Nanosecond,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID: "channel-1",
		PostID:    "post-pvc-quota",
		UserID:    "owner",
		UserName:  "owner",
		Message:   "Keep retained session data when PVC quota is exhausted.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if pvcCreateAttempts != 1 {
		t.Fatalf("session PVC create attempts = %d, want 1", pvcCreateAttempts)
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "pods" && action.GetVerb() == "delete" {
			t.Fatalf("PVC quota path deleted a session pod: %#v", action)
		}
	}
	oldest := store.agentSessions["oldest-idle"]
	if oldest.PodName != "mc-session-oldest-idle" || oldest.KubernetesNamespace != "mattermost" {
		t.Fatalf("PVC quota path evicted idle session: %#v", oldest)
	}
	if _, err := client.CoreV1().Pods("mattermost").Get(context.Background(), oldest.PodName, metav1.GetOptions{}); err != nil {
		t.Fatalf("idle session pod was removed: %v", err)
	}
}

type codexReadyRuntimeRunner struct {
	runtimerepo.Runner
}

func (runner codexReadyRuntimeRunner) CheckCodexAuthSecret(_ context.Context, input runtimerepo.CodexAuthSecretCheckInput) (runtimerepo.CodexAuthSecretCheckResult, error) {
	return runtimerepo.CodexAuthSecretCheckResult{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		Ready:       true,
	}, nil
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
	reconciler := &fakeAutomationRuntimeReconciler{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:                   testLocalizer(t, texti18n.DefaultLocale),
		Store:                       store,
		RuntimeRunner:               runner,
		StorageReady:                true,
		RuntimeReady:                true,
		DisableMonitor:              true,
		AutomationRuntimeReconciler: reconciler,
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
	if store.exactGuardCalls == 0 || store.completeTurnCalls != 1 || store.completeTurnInput.SessionID != 1 || store.completeTurnInput.TurnID != 1 || store.completeTurnInput.RunID != "run-1" || store.completeTurnInput.ExpectedStatus != agentSessionTurnRunning {
		t.Fatalf("repair completion fence=%d calls=%d input=%#v", store.exactGuardCalls, store.completeTurnCalls, store.completeTurnInput)
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
	if reconciler.calls != 1 || len(reconciler.commands) != 1 {
		t.Fatalf("automation reconciler calls = %d commands = %#v", reconciler.calls, reconciler.commands)
	}
	command := reconciler.commands[0]
	if command.ProjectID != 1 || command.RuntimeSessionID != 1 || command.RuntimeTurnID != 1 || command.RuntimeRunID != "run-1" || command.RuntimeStatus != agentSessionTurnFailed {
		t.Fatalf("automation reconcile command = %#v", command)
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

func TestCodexAuthCheckReclaimsOldestIdleSessionOnCapacityPressure(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", Enabled: true}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", Slug: "manager"}
	store.agentSessions = map[string]entity.AgentSession{
		"oldest-idle": {
			ID: 1, SessionKey: "oldest-idle", ProjectID: 1, ChatID: 1, RoleID: 1,
			Status: agentSessionStatusIdle, KubernetesNamespace: "mattermost", PodName: "mc-session-oldest-idle",
			PVCName: "mc-session-ws-oldest-idle", TokenSecretRef: "matter-codex-session-oldest-idle",
			LastActivityAt: time.Now().Add(-4 * time.Hour),
		},
	}
	capacityErr := runtimerepo.NewAgentSessionCapacityError("test scheduler pressure", errors.New("insufficient cpu"))
	runner := &fakeRuntimeRunner{authSecretCheckErrors: []error{capacityErr, capacityErr, nil}}
	svc := NewChatRunService(ChatRunServiceConfig{
		Store: store, RuntimeRunner: runner, StorageReady: true, RuntimeReady: true,
		DisableMonitor: true, CapacityRetryDelay: time.Nanosecond,
	})

	result, err := svc.checkCodexAuthSecretWithCapacityReclaim(context.Background(), runtimerepo.CodexAuthSecretCheckInput{
		AccountName: "main", SecretName: "matter-codex-codex-auth-main",
	})
	if err != nil || !result.Ready {
		t.Fatalf("check result=%#v error=%v", result, err)
	}
	if runner.authSecretChecks != 3 || len(runner.cleanedSessionKeys) != 1 || runner.cleanedSessionKeys[0] != "oldest-idle" {
		t.Fatalf("checks=%d cleanup=%#v", runner.authSecretChecks, runner.cleanedSessionKeys)
	}
	if session := store.agentSessions["oldest-idle"]; session.PodName != "" || session.PVCName == "" || session.TokenSecretRef == "" {
		t.Fatalf("idle session state = %#v", session)
	}
}

func TestCodexAuthCheckDoesNotEvictSessionQueuedAfterHealthCheck(t *testing.T) {
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", Enabled: true}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Manager", Slug: "manager"}
	store.agentSessions = map[string]entity.AgentSession{
		"idle": {
			ID: 1, SessionKey: "idle", ProjectID: 1, ChatID: 1, RoleID: 1,
			Status: agentSessionStatusIdle, KubernetesNamespace: "mattermost", PodName: "mc-session-idle",
			PVCName: "mc-session-ws-idle", TokenSecretRef: "matter-codex-session-idle",
			LastActivityAt: time.Now().Add(-4 * time.Hour),
		},
	}
	store.beforeIdlePodEviction = func() {
		store.sessionTurns = append(store.sessionTurns, entity.AgentSessionTurn{ID: 1, SessionID: 1, RunID: "queued-during-health-check", Status: agentSessionTurnQueued})
		store.beforeIdlePodEviction = nil
	}
	capacityErr := runtimerepo.NewAgentSessionCapacityError("test scheduler pressure", errors.New("insufficient cpu"))
	runner := &fakeRuntimeRunner{authSecretCheckErrors: []error{capacityErr, capacityErr}}
	svc := NewChatRunService(ChatRunServiceConfig{
		Store: store, RuntimeRunner: runner, StorageReady: true, RuntimeReady: true,
		DisableMonitor: true, CapacityRetryDelay: time.Nanosecond,
	})

	_, err := svc.checkCodexAuthSecretWithCapacityReclaim(context.Background(), runtimerepo.CodexAuthSecretCheckInput{
		AccountName: "main", SecretName: "matter-codex-codex-auth-main",
	})
	if !runtimerepo.IsReclaimableAgentSessionCapacityError(err) {
		t.Fatalf("capacity error = %v", err)
	}
	if runner.sessionRuntimeHealthCalls != 1 || len(runner.cleanedSessionKeys) != 0 {
		t.Fatalf("health=%d cleanup=%#v", runner.sessionRuntimeHealthCalls, runner.cleanedSessionKeys)
	}
	if session := store.agentSessions["idle"]; session.PodName != "mc-session-idle" || session.KubernetesNamespace != "mattermost" {
		t.Fatalf("busy session pod was evicted: %#v", session)
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
	for _, action := range publisher.cards[0].Actions[:2] {
		state := threadRepositoryStateFromCardAction(t, action)
		if got := contextStringValue(action.Context, interactionCapabilityResourceIDKey); got != fmt.Sprint(state.ThreadContextID) {
			t.Fatalf("capability resource id = %q for context %#v", got, action.Context)
		}
		if got := contextStringValue(action.Context, "kind"); got != "agents_menu" {
			t.Fatalf("interaction kind = %q for context %#v", got, action.Context)
		}
		resourceType, resourceID := interactionResource(action.Context)
		if !typedInteractionOperationAllowed(InteractionAdmissionRequest{
			ActionKey:    "mattermost.callback.action",
			Operation:    actionCallbackOperation(action.Context),
			ResourceType: resourceType,
			ResourceID:   resourceID,
		}) {
			t.Fatalf("thread repository action is rejected by typed admission: %#v", action.Context)
		}
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

func threadRepositoryStateFromCardAction(t *testing.T, action MattermostCardAction) threadRepositorySelectionState {
	t.Helper()
	state, ok := parseThreadRepositorySelectionResourceID(contextStringValue(action.Context, "resource_id"))
	if !ok {
		t.Fatalf("thread repository action context = %#v", action.Context)
	}
	return state
}

func TestChatRunRestoresMissingThreadContextFromExistingSession(t *testing.T) {
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
		GitHubAccountName: "agent",
		Enabled:           true,
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Management", ChatType: "manager"}
	store.setChatBindings(1, []int64{1}, nil)
	rootPostID := "manager-root"
	sessionKey := agentSessionKey(1, 1, agentSessionScopeThreadRole, rootPostID)
	store.agentSessions = map[string]entity.AgentSession{
		sessionKey: {
			ID:                   1,
			SessionKey:           sessionKey,
			ProjectID:            1,
			ChatID:               1,
			RoleID:               1,
			SessionScope:         agentSessionScopeThreadRole,
			MattermostChannelID:  "channel-1",
			MattermostRootPostID: rootPostID,
			OpenAIAccountName:    "main",
			CodexSessionID:       "codex-session-1",
			Status:               agentSessionStatusIdle,
			Capabilities:         `{"repositories":[{"provider":"github","owner":"codex-k8s","name":"matter-codex","default_branch":"main"}]}`,
			TTLSeconds:           defaultThreadSessionTTLSeconds,
		},
	}
	runner := &fakeRuntimeRunner{}
	publisher := &fakeThreadPublisher{}
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://matter-codex.example/mattermost/actions/agents",
		StorageReady:    true,
		RuntimeReady:    true,
		DisableMonitor:  true,
	})

	result := svc.HandleChatPost(context.Background(), ChatPostCommand{
		ChannelID:  "channel-1",
		PostID:     "reply-1",
		RootPostID: rootPostID,
		UserID:     "owner",
		UserName:   "owner",
		Message:    "Continue the existing manager session.",
	})

	if result.RunID == "" || result.Mode != "session" {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.cards) != 1 || publisher.cards[0].Message != "matter-codex agent turn status #notrigger" {
		t.Fatalf("existing session must not prompt for repository selection: %#v", publisher.cards)
	}
	threadContext, err := store.GetThreadContext(context.Background(), 1, rootPostID)
	if err != nil || threadContext.Status != threadContextStatusConfigured || threadContext.RepositoryID != 1 {
		t.Fatalf("thread context = %#v err=%v", threadContext, err)
	}
	if len(runner.sessionRuns) != 1 || runner.sessionRuns[0].RepositoryOwner != "codex-k8s" || runner.sessionRuns[0].RepositoryName != "matter-codex" {
		t.Fatalf("session runs = %#v", runner.sessionRuns)
	}
	if len(store.sessionTurns) != 1 || !strings.Contains(store.sessionTurns[0].Message, "Continue the existing manager session.") {
		t.Fatalf("turns = %#v", store.sessionTurns)
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
	postErr              error
	postErrors           []error
	cardPostErr          error
	cardUpdateErr        error
	postWithTokenErr     error
	updateWithTokenErr   error
	postWithTokenCalls   int
	updateWithTokenCalls int
	beforeUpdate         func()
}

func (publisher *fakeThreadPublisher) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	if len(publisher.postErrors) > 0 {
		err := publisher.postErrors[0]
		publisher.postErrors = publisher.postErrors[1:]
		if err != nil {
			return MattermostPostRef{}, err
		}
	}
	if publisher.postErr != nil {
		return MattermostPostRef{}, publisher.postErr
	}
	publisher.posts = append(publisher.posts, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "reply-" + input.RootPostID}, nil
}

func (publisher *fakeThreadPublisher) ReconcileOrPostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	for index, post := range publisher.posts {
		if post.IdempotencyID == input.IdempotencyID && input.IdempotencyID != "" {
			return MattermostPostRef{ChannelID: post.ChannelID, PostID: fmt.Sprintf("idempotent-post-%d", index+1)}, nil
		}
	}
	publisher.posts = append(publisher.posts, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: fmt.Sprintf("idempotent-post-%d", len(publisher.posts))}, nil
}

func (publisher *fakeThreadPublisher) PostThreadMessageWithToken(_ context.Context, _ string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.postWithTokenCalls++
	if publisher.postWithTokenErr != nil {
		return MattermostPostRef{}, publisher.postWithTokenErr
	}
	return publisher.PostThreadMessage(context.Background(), input)
}

func (publisher *fakeThreadPublisher) UpdateThreadMessage(_ context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	if publisher.beforeUpdate != nil {
		publisher.beforeUpdate()
	}
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
	if publisher.cardPostErr != nil {
		return MattermostPostRef{}, publisher.cardPostErr
	}
	publisher.cards = append(publisher.cards, card)
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: "card-" + card.RootPostID}, nil
}

func (publisher *fakeThreadPublisher) UpdateThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	if publisher.cardUpdateErr != nil {
		return MattermostPostRef{}, publisher.cardUpdateErr
	}
	publisher.cardUpdates = append(publisher.cardUpdates, card)
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: card.PostID}, nil
}

func (publisher *fakeThreadPublisher) AddPostReactionWithToken(_ context.Context, token string, input MattermostPostReactionInput) error {
	publisher.reactionTokens = append(publisher.reactionTokens, token)
	publisher.reactions = append(publisher.reactions, input)
	return nil
}

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
