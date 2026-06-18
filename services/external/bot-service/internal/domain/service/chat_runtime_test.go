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

	if result.RunID == "" || result.Mode != chatRunModeChat {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedChatRunID != result.RunID || runner.chatCodexSecret != "matter-codex-codex-auth-main" {
		t.Fatalf("chat runner = %#v", runner.chatRuns)
	}
	if len(publisher.posts) != 1 || publisher.posts[0].RootPostID != "post-1" || !strings.Contains(publisher.posts[0].Message, "agent run started") {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if !strings.Contains(runner.chatRuns[0].Prompt, "Help me decompose the task.") || !strings.Contains(runner.chatRuns[0].Prompt, "Project: Platform") {
		t.Fatalf("prompt = %q", runner.chatRuns[0].Prompt)
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

	if result.RunID == "" || result.Mode != chatRunModeDeveloper {
		t.Fatalf("result = %#v", result)
	}
	if runner.startedDeveloperRunID != result.RunID || runner.developerGitHubSecret != "matter-codex-github-agent" {
		t.Fatalf("developer runner = %#v", runner.developerRuns)
	}
	if runner.developerBaseBranch != "main" || !strings.HasPrefix(runner.developerHeadBranch, "matter-codex-chat-1-") {
		t.Fatalf("developer branch = base %q head %q", runner.developerBaseBranch, runner.developerHeadBranch)
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

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
