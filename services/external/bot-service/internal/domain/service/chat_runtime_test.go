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
		openAIAccounts: map[string]entity.OpenAIAccount{
			"main": {Name: "main", SecretRef: "matter-codex-codex-auth-main", Status: "authorized"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured", Username: "agent", Email: "agent@example.invalid"},
		},
	}
}

type fakeThreadPublisher struct {
	posts []MattermostThreadPostInput
}

func (publisher *fakeThreadPublisher) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.posts = append(publisher.posts, input)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "reply-" + input.RootPostID}, nil
}

func (publisher *fakeThreadPublisher) PostThreadMessageWithToken(_ context.Context, _ string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return publisher.PostThreadMessage(context.Background(), input)
}

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
