package service

import (
	"context"
	"strings"
	"testing"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

func TestSlashTokenCheck(t *testing.T) {
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		GitHubTokenConfigured: true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})
	text := svc.Handle(context.Background(), SlashCommand{Text: "token check"})
	for _, want := range []string{
		"mattermost bot token: configured",
		"mattermost slash token: configured",
		"github token: configured",
		"database dsn: configured",
		"storage: ready",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Handle(token check) missing %q in %q", want, text)
		}
	}
}

func TestSlashRepoAddCreatesChannelAndStoresRepository(t *testing.T) {
	store := &fakeAdminStore{}
	channels := &fakeChannelManager{}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		Store:                 store,
		ChannelManager:        channels,
		DefaultTeamName:       "agents",
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "repo add github codex-k8s/matter-codex main", UserName: "owner"})

	if !strings.Contains(text, "github:codex-k8s/matter-codex") {
		t.Fatalf("Handle(repo add) text = %q", text)
	}
	if store.upsert.Provider != "github" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "matter-codex" {
		t.Fatalf("unexpected upsert input: %#v", store.upsert)
	}
	if channels.channelName != "repo-codex-k8s-matter-codex" {
		t.Fatalf("channelName = %q", channels.channelName)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
}

func TestSlashGitHubCheckUsesProvider(t *testing.T) {
	store := &fakeAdminStore{}
	provider := &fakeRepositoryProvider{}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		Store:                 store,
		RepositoryProvider:    provider,
		GitHubTokenConfigured: true,
		StorageReady:          true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github check codex-k8s/matter-codex", UserName: "owner"})

	if !strings.Contains(text, "matter-codex GitHub repo access") || !strings.Contains(text, "github:codex-k8s/matter-codex") {
		t.Fatalf("Handle(github check) text = %q", text)
	}
	if provider.checkedOwner != "codex-k8s" || provider.checkedName != "matter-codex" {
		t.Fatalf("unexpected provider call: %#v", provider)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
}

func TestSlashGitHubBranchDryRun(t *testing.T) {
	provider := &fakeRepositoryProvider{}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		RepositoryProvider:    provider,
		GitHubTokenConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github branch dry-run codex-k8s/matter-codex smoke-branch main"})

	if !strings.Contains(text, "GitHub branch dry-run") || !strings.Contains(text, "changes: none") {
		t.Fatalf("Handle(github branch dry-run) text = %q", text)
	}
	if provider.resolvedBranch != "main" {
		t.Fatalf("resolvedBranch = %q", provider.resolvedBranch)
	}
}

func TestSlashGitHubPRStatus(t *testing.T) {
	provider := &fakeRepositoryProvider{}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		RepositoryProvider:    provider,
		GitHubTokenConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github pr status codex-k8s/matter-codex 4"})

	if !strings.Contains(text, "GitHub PR status") || !strings.Contains(text, "`#4`") || !strings.Contains(text, "review: `APPROVED`") {
		t.Fatalf("Handle(github pr status) text = %q", text)
	}
	if provider.prNumber != 4 {
		t.Fatalf("prNumber = %d", provider.prNumber)
	}
}

func TestSlashProfileList(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Description: "dev", Enabled: true}},
	}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService: testStatusService(),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "profile list"})

	if !strings.Contains(text, "`developer` role `developer` enabled") {
		t.Fatalf("Handle(profile list) text = %q", text)
	}
}

func testStatusService() *StatusService {
	return NewStatusService(Config{
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DatabaseConfigured:   true,
		StorageReady:         true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control"},
	})
}

type fakeAdminStore struct {
	upsert        adminrepo.UpsertRepositoryInput
	auditRecorded bool
	profiles      []entity.AgentProfile
}

func (store *fakeAdminStore) UpsertRepository(_ context.Context, input adminrepo.UpsertRepositoryInput) (entity.Repository, bool, error) {
	store.upsert = input
	return entity.Repository{
		ID:                1,
		Provider:          input.Provider,
		Owner:             input.Owner,
		Name:              input.Name,
		DefaultBranch:     input.DefaultBranch,
		Status:            "active",
		MattermostChannel: input.MattermostChannel,
	}, true, nil
}

func (store *fakeAdminStore) ListRepositories(context.Context, int) ([]entity.Repository, error) {
	return []entity.Repository{}, nil
}

func (store *fakeAdminStore) ListAgentProfiles(context.Context) ([]entity.AgentProfile, error) {
	return store.profiles, nil
}

func (store *fakeAdminStore) RecordAuditEvent(context.Context, adminrepo.AuditEventInput) error {
	store.auditRecorded = true
	return nil
}

type fakeChannelManager struct {
	channelName string
}

func (manager *fakeChannelManager) EnsureRepositoryChannel(_ context.Context, _ string, channelName string, _ string) (bool, error) {
	manager.channelName = channelName
	return true, nil
}

type fakeRepositoryProvider struct {
	checkedOwner   string
	checkedName    string
	resolvedBranch string
	createdBranch  string
	prNumber       int
}

func (provider *fakeRepositoryProvider) CheckRepository(_ context.Context, owner string, name string) (providerrepo.RepositoryAccess, error) {
	provider.checkedOwner = owner
	provider.checkedName = name
	return providerrepo.RepositoryAccess{
		Provider:      "github",
		Owner:         owner,
		Name:          name,
		DefaultBranch: "main",
		Private:       true,
		CanPull:       true,
		CanPush:       true,
	}, nil
}

func (provider *fakeRepositoryProvider) ResolveBranch(_ context.Context, owner string, name string, branch string) (providerrepo.BranchRef, error) {
	provider.resolvedBranch = branch
	return providerrepo.BranchRef{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		Branch:   branch,
		SHA:      "1234567890abcdef",
	}, nil
}

func (provider *fakeRepositoryProvider) CreateBranch(_ context.Context, owner string, name string, branch string, _ string) (providerrepo.BranchRef, error) {
	provider.createdBranch = branch
	return providerrepo.BranchRef{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		Branch:   branch,
		SHA:      "1234567890abcdef",
		Created:  true,
	}, nil
}

func (provider *fakeRepositoryProvider) PreviewPullRequest(_ context.Context, input providerrepo.PullRequestInput) (providerrepo.PullRequestPreview, error) {
	return providerrepo.PullRequestPreview{
		Provider: "github",
		Owner:    input.Owner,
		Name:     input.Name,
		Head:     input.Head,
		Base:     input.Base,
		Title:    input.Title,
		HeadSHA:  "abcdef1234567890",
		BaseSHA:  "1234567890abcdef",
	}, nil
}

func (provider *fakeRepositoryProvider) CreatePullRequest(_ context.Context, input providerrepo.PullRequestInput) (providerrepo.PullRequestSummary, error) {
	return providerrepo.PullRequestSummary{
		Provider: "github",
		Owner:    input.Owner,
		Name:     input.Name,
		Number:   10,
		Title:    input.Title,
		State:    "open",
		URL:      "https://github.example/pr/10",
		Draft:    true,
	}, nil
}

func (provider *fakeRepositoryProvider) GetPullRequest(_ context.Context, owner string, name string, number int) (providerrepo.PullRequestSummary, error) {
	provider.prNumber = number
	return providerrepo.PullRequestSummary{
		Provider:           "github",
		Owner:              owner,
		Name:               name,
		Number:             number,
		Title:              "test pr",
		State:              "open",
		URL:                "https://github.example/pr/4",
		ReviewCount:        1,
		ReviewCommentCount: 2,
		LatestReviews: []providerrepo.PullRequestReview{
			{State: "APPROVED", Author: "reviewer"},
		},
	}, nil
}
