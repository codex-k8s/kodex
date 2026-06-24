package service

import (
	"context"
	"strings"
	"testing"

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
	if len(publisher.posts) != 1 || publisher.posts[0].RootPostID != "post-1" || !strings.Contains(publisher.posts[0].Message, "queued agent session turn") {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if len(store.sessionTurns) != 1 || !strings.Contains(store.sessionTurns[0].Message, "Help me decompose the task.") || !strings.Contains(store.sessionTurns[0].Message, "Project: Platform") {
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
	if !strings.Contains(secondPrompt, "# User message") || !strings.Contains(secondPrompt, "Follow-up task.") || !strings.Contains(secondPrompt, "Continue the existing Codex session") {
		t.Fatalf("second prompt = %q", secondPrompt)
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
	posts   []MattermostThreadPostInput
	updates []MattermostThreadUpdateInput
	cards   []MattermostCard
}

func (publisher *fakeThreadPublisher) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.posts = append(publisher.posts, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "reply-" + input.RootPostID}, nil
}

func (publisher *fakeThreadPublisher) PostThreadMessageWithToken(_ context.Context, _ string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return publisher.PostThreadMessage(context.Background(), input)
}

func (publisher *fakeThreadPublisher) UpdateThreadMessage(_ context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	publisher.updates = append(publisher.updates, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: input.PostID}, nil
}

func (publisher *fakeThreadPublisher) UpdateThreadMessageWithToken(_ context.Context, _ string, input MattermostThreadUpdateInput) (MattermostPostRef, error) {
	return publisher.UpdateThreadMessage(context.Background(), input)
}

func (publisher *fakeThreadPublisher) PostThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	publisher.cards = append(publisher.cards, card)
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: "card-" + card.RootPostID}, nil
}

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
