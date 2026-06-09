package service

import (
	"context"
	"strings"
	"testing"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestSlashTokenCheck(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
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
		"github webhook secret: missing",
		"database dsn: configured",
		"storage: ready",
		"kubernetes runtime: missing",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Handle(token check) missing %q in %q", want, text)
		}
	}
}

func TestSlashRepoAddCreatesChannelAndStoresRepository(t *testing.T) {
	store := &fakeAdminStore{}
	channels := &fakeChannelManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
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

func TestSlashRepoAddEnsuresGitHubWebhook(t *testing.T) {
	store := &fakeAdminStore{}
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:               localizer,
		StatusService:           testStatusService(localizer),
		Store:                   store,
		RepositoryProvider:      provider,
		GitHubTokenConfigured:   true,
		GitHubWebhookConfigured: true,
		StorageReady:            true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "repo add github codex-k8s/matter-codex main", UserName: "owner"})

	if !strings.Contains(text, "webhook: `created` id `99` active `true`") {
		t.Fatalf("Handle(repo add) text = %q", text)
	}
	if provider.webhookOwner != "codex-k8s" || provider.webhookName != "matter-codex" {
		t.Fatalf("webhook target = %s/%s", provider.webhookOwner, provider.webhookName)
	}
}

func TestSlashGitHubCheckUsesProvider(t *testing.T) {
	store := &fakeAdminStore{}
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
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
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
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
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
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

func TestSlashGitHubWebhookEnsure(t *testing.T) {
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:               localizer,
		StatusService:           testStatusService(localizer),
		RepositoryProvider:      provider,
		GitHubTokenConfigured:   true,
		GitHubWebhookConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github webhook ensure codex-k8s/matter-codex"})

	if !strings.Contains(text, "matter-codex GitHub webhook") || !strings.Contains(text, "webhook: `created`") {
		t.Fatalf("Handle(github webhook ensure) text = %q", text)
	}
	if provider.webhookOwner != "codex-k8s" || provider.webhookName != "matter-codex" {
		t.Fatalf("webhook target = %s/%s", provider.webhookOwner, provider.webhookName)
	}
}

func TestSlashProfileList(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Description: "dev", Enabled: true}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "profile list"})

	if !strings.Contains(text, "`developer` role `developer` enabled") {
		t.Fatalf("Handle(profile list) text = %q", text)
	}
}

func TestSlashLocaleSetChangesResponses(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "locale set ru"})
	if !strings.Contains(text, "локаль изменена") {
		t.Fatalf("Handle(locale set ru) text = %q", text)
	}
	text = svc.Handle(context.Background(), SlashCommand{Text: "token check"})
	if !strings.Contains(text, "mattermost bot token: настроен") || !strings.Contains(text, "storage: готов") {
		t.Fatalf("Handle(token check) text = %q", text)
	}
	text = svc.Handle(context.Background(), SlashCommand{Text: "locale set en"})
	if !strings.Contains(text, "locale changed to `en`") {
		t.Fatalf("Handle(locale set en) text = %q", text)
	}
}

func TestSlashRuntimeSmokeStatusAndCleanup(t *testing.T) {
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		RuntimeRunner:     runner,
		RuntimeConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "runtime smoke smoke-test"})
	if !strings.Contains(text, "Kubernetes smoke runner started") || !strings.Contains(text, "`smoke-test`") {
		t.Fatalf("Handle(runtime smoke) text = %q", text)
	}
	if runner.startedRunID != "smoke-test" {
		t.Fatalf("startedRunID = %q", runner.startedRunID)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "runtime status smoke-test"})
	if !strings.Contains(text, "Kubernetes run status") || !strings.Contains(text, "phase `Succeeded`") || !strings.Contains(text, "smoke-ok") {
		t.Fatalf("Handle(runtime status) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "runtime cleanup smoke-test"})
	if !strings.Contains(text, "Kubernetes run cleanup") || !strings.Contains(text, "job deleted: `true`") {
		t.Fatalf("Handle(runtime cleanup) text = %q", text)
	}
	if runner.cleanedRunID != "smoke-test" {
		t.Fatalf("cleanedRunID = %q", runner.cleanedRunID)
	}
}

func TestSlashRuntimeRejectsInvalidRunID(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		RuntimeRunner:     &fakeRuntimeRunner{},
		RuntimeConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "runtime smoke Bad_ID"})
	if !strings.Contains(text, "run id must be lowercase") {
		t.Fatalf("Handle(runtime smoke Bad_ID) text = %q", text)
	}
}

func TestSlashDevSmokeStatusAndCleanup(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Enabled: true}},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		Store:                 store,
		RuntimeRunner:         runner,
		GitHubTokenConfigured: true,
		StorageReady:          true,
		RuntimeConfigured:     true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "dev smoke codex-k8s/matter-codex dev-test", UserName: "owner"})
	if !strings.Contains(text, "developer smoke run started") || !strings.Contains(text, "`dev-test`") {
		t.Fatalf("Handle(dev smoke) text = %q", text)
	}
	if runner.startedDeveloperRunID != "dev-test" || runner.developerHeadBranch != "matter-codex-dev-dev-test" {
		t.Fatalf("runner = %#v", runner)
	}
	if store.agentRun.RunID != "dev-test" || store.agentRun.ProfileName != "developer" {
		t.Fatalf("agentRun = %#v", store.agentRun)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "dev status dev-test"})
	if !strings.Contains(text, "developer run status") || !strings.Contains(text, "pr-url") || !strings.Contains(text, "https://github.example/pr/10") {
		t.Fatalf("Handle(dev status) text = %q", text)
	}
	if store.updatedRunStatus != "pr_created" || store.updatedPRURL != "https://github.example/pr/10" {
		t.Fatalf("updated run = %q %q", store.updatedRunStatus, store.updatedPRURL)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "dev cleanup dev-test", UserName: "owner"})
	if !strings.Contains(text, "developer run cleanup") || !strings.Contains(text, "pvc deleted: `true`") {
		t.Fatalf("Handle(dev cleanup) text = %q", text)
	}
}

func testLocalizer(t *testing.T, locale string) *texti18n.Localizer {
	t.Helper()
	localizer, err := texti18n.New(locale)
	if err != nil {
		t.Fatalf("New localizer error = %v", err)
	}
	return localizer
}

func testStatusService(localizer *texti18n.Localizer) *StatusService {
	return NewStatusService(Config{
		Localizer:            localizer,
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DatabaseConfigured:   true,
		StorageReady:         true,
		RuntimeConfigured:    true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control"},
	})
}

type fakeRuntimeRunner struct {
	startedRunID          string
	startedDeveloperRunID string
	developerHeadBranch   string
	cleanedRunID          string
}

func (runner *fakeRuntimeRunner) StartSmokeRun(_ context.Context, input runtimerepo.SmokeRunInput) (runtimerepo.StartedRun, error) {
	runner.startedRunID = input.RunID
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartDeveloperRun(_ context.Context, input runtimerepo.DeveloperRunInput) (runtimerepo.StartedRun, error) {
	runner.startedDeveloperRunID = input.RunID
	runner.developerHeadBranch = input.HeadBranch
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) GetRunStatus(_ context.Context, runID string) (runtimerepo.RunStatus, error) {
	return runtimerepo.RunStatus{
		RunID:        runID,
		Namespace:    "mattermost",
		JobName:      "mc-run-" + runID,
		PVCName:      "mc-ws-" + runID,
		PodName:      "mc-run-" + runID + "-pod",
		Exists:       true,
		JobSucceeded: 1,
		PodPhase:     "Succeeded",
		LogTail:      "matter-codex smoke done\nmatter-codex artifact pr-url: https://github.example/pr/10\nsmoke-ok",
		Artifacts:    map[string]string{"pr-url": "https://github.example/pr/10"},
	}, nil
}

func (runner *fakeRuntimeRunner) CleanupRun(_ context.Context, runID string) (runtimerepo.CleanupResult, error) {
	runner.cleanedRunID = runID
	return runtimerepo.CleanupResult{
		RunID:      runID,
		Namespace:  "mattermost",
		JobDeleted: true,
		PVCDeleted: true,
	}, nil
}

type fakeAdminStore struct {
	upsert           adminrepo.UpsertRepositoryInput
	auditRecorded    bool
	profiles         []entity.AgentProfile
	agentRun         entity.AgentRun
	updatedRunStatus string
	updatedPRURL     string
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

func (store *fakeAdminStore) CreateAgentRun(_ context.Context, input adminrepo.CreateAgentRunInput) (entity.AgentRun, error) {
	store.agentRun = entity.AgentRun{
		ID:                  1,
		RunID:               input.RunID,
		ProfileName:         input.ProfileName,
		Role:                input.Role,
		Provider:            input.Provider,
		Owner:               input.Owner,
		Name:                input.Name,
		BaseBranch:          input.BaseBranch,
		HeadBranch:          input.HeadBranch,
		Status:              input.Status,
		KubernetesNamespace: input.KubernetesNamespace,
		JobName:             input.JobName,
		PVCName:             input.PVCName,
		Summary:             input.Summary,
	}
	return store.agentRun, nil
}

func (store *fakeAdminStore) GetAgentRun(_ context.Context, runID string) (entity.AgentRun, error) {
	if store.agentRun.RunID == "" {
		store.agentRun = entity.AgentRun{
			ID:          1,
			RunID:       runID,
			ProfileName: "developer",
			Role:        "developer",
			Provider:    "github",
			Owner:       "codex-k8s",
			Name:        "matter-codex",
			BaseBranch:  "main",
			HeadBranch:  "matter-codex-dev-" + runID,
			Status:      "started",
		}
	}
	return store.agentRun, nil
}

func (store *fakeAdminStore) UpdateAgentRunArtifacts(_ context.Context, input adminrepo.UpdateAgentRunArtifactsInput) (entity.AgentRun, error) {
	store.updatedRunStatus = input.Status
	store.updatedPRURL = input.PRURL
	store.agentRun.Status = input.Status
	store.agentRun.PRURL = input.PRURL
	return store.agentRun, nil
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
	webhookOwner   string
	webhookName    string
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

func (provider *fakeRepositoryProvider) EnsureRepositoryWebhook(_ context.Context, owner string, name string) (providerrepo.WebhookRegistration, error) {
	provider.webhookOwner = owner
	provider.webhookName = name
	return providerrepo.WebhookRegistration{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		ID:       99,
		URL:      "https://matter-codex.example/github/webhook",
		Events:   []string{"pull_request", "push"},
		Created:  true,
		Active:   true,
	}, nil
}
