package service

import (
	"context"
	"fmt"
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
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Description: "dev", Enabled: true, OpenAIAccountName: "primary", GitHubAccountName: "agent"}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "profile list"})

	if !strings.Contains(text, "`developer` role `developer` openai `primary` github `agent` enabled") {
		t.Fatalf("Handle(profile list) text = %q", text)
	}
}

func TestSlashOpenAIAuthStatusAndList(t *testing.T) {
	store := &fakeAdminStore{}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:           localizer,
		StatusService:       testStatusService(localizer),
		Store:               store,
		RuntimeRunner:       runner,
		CodexAuthSecretName: "matter-codex-codex-auth",
		StorageReady:        true,
		RuntimeConfigured:   true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "openai auth primary", UserName: "owner"})
	if !strings.Contains(text, "OpenAI account created") || !strings.Contains(text, "`primary`") {
		t.Fatalf("Handle(openai auth) text = %q", text)
	}
	if runner.authAccount != "primary" || runner.authSecret != "matter-codex-codex-auth-primary" {
		t.Fatalf("auth session = %q %q", runner.authAccount, runner.authSecret)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "openai status primary", UserName: "owner"})
	if !strings.Contains(text, "https://auth.openai.com/codex/device") || !strings.Contains(text, "`ABCD-12345`") {
		t.Fatalf("Handle(openai status) text = %q", text)
	}

	runner.authReady = true
	text = svc.Handle(context.Background(), SlashCommand{Text: "openai status primary", UserName: "owner"})
	if !strings.Contains(text, "OpenAI account authorized") || !strings.Contains(text, "matter-codex-codex-auth-primary") {
		t.Fatalf("Handle(openai status ready) text = %q", text)
	}
	account, ok := store.openAIAccounts["primary"]
	if !ok || account.Status != "authorized" {
		t.Fatalf("account = %#v ok=%v", account, ok)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "openai list"})
	if !strings.Contains(text, "`primary` status `authorized`") {
		t.Fatalf("Handle(openai list) text = %q", text)
	}
}

func TestSlashPromptSetRenderShowAndList(t *testing.T) {
	store := &fakeAdminStore{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "prompt help reviewer review_pr"})
	if !strings.Contains(text, "{{.Repository.FullName}}") || !strings.Contains(text, "{{now}}") {
		t.Fatalf("Handle(prompt help) text = %q", text)
	}

	templateBody := "DECISION: comment\nRepo: {{.Repository.FullName}}\nRun: {{.Run.ID}}"
	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt set reviewer review_pr " + templateBody, UserName: "owner"})
	if !strings.Contains(text, "prompt template updated") || !strings.Contains(text, "codex-k8s/matter-codex") {
		t.Fatalf("Handle(prompt set) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt show reviewer review_pr"})
	if !strings.Contains(text, templateBody) {
		t.Fatalf("Handle(prompt show) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt render reviewer review_pr"})
	if !strings.Contains(text, "prompt template render OK") || !strings.Contains(text, "Repo: codex-k8s/matter-codex") {
		t.Fatalf("Handle(prompt render) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt list reviewer"})
	if !strings.Contains(text, "`reviewer/review_pr`") {
		t.Fatalf("Handle(prompt list) text = %q", text)
	}
}

func TestSlashPromptHelpUsesLocale(t *testing.T) {
	localizer := testLocalizer(t, texti18n.RussianLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         &fakeAdminStore{},
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "prompt help reviewer review_pr"})
	if !strings.Contains(text, "Доступные placeholders") || !strings.Contains(text, "{{.Repository.FullName}}") || !strings.Contains(text, "{{now}}") {
		t.Fatalf("Handle(prompt help) text = %q", text)
	}
}

func TestSlashPromptRenderUsesSelectedLocale(t *testing.T) {
	store := &fakeAdminStore{
		promptTemplates: map[string]entity.AgentPromptTemplate{
			promptTemplateMapKey("reviewer", reviewPRTemplateKey): {
				ID:          1,
				ProfileName: "reviewer",
				TemplateKey: reviewPRTemplateKey,
				Body:        "Language: {{.Locale.Language}}\nLocale: {{.Locale.Code}}",
			},
		},
	}
	localizer := testLocalizer(t, texti18n.RussianLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "prompt render reviewer review_pr"})
	if !strings.Contains(text, "Language: Russian") || !strings.Contains(text, "Locale: ru") {
		t.Fatalf("Handle(prompt render ru) text = %q", text)
	}

	if _, err := localizer.SetLocale(texti18n.DefaultLocale); err != nil {
		t.Fatalf("SetLocale(en) error = %v", err)
	}
	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt render reviewer review_pr"})
	if !strings.Contains(text, "Language: English") || !strings.Contains(text, "Locale: en") {
		t.Fatalf("Handle(prompt render en) text = %q", text)
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
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Enabled: true, OpenAIAccountName: "primary", GitHubAccountName: "agent"}},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized", SecretRef: "matter-codex-codex-auth-primary"},
		},
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
	if runner.developerCodexSecret != "matter-codex-codex-auth-primary" {
		t.Fatalf("developerCodexSecret = %q", runner.developerCodexSecret)
	}
	if runner.developerGitHubSecret != "matter-codex-github-agent" {
		t.Fatalf("developerGitHubSecret = %q", runner.developerGitHubSecret)
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

func TestSlashReviewPRStatusAndCleanup(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "reviewer", Role: "reviewer", Enabled: true, OpenAIAccountName: "primary", GitHubAccountName: "primary"}},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized", SecretRef: "matter-codex-codex-auth-primary"},
		},
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

	text := svc.Handle(context.Background(), SlashCommand{Text: "review pr codex-k8s/matter-codex 12 review-test", UserName: "owner"})
	if !strings.Contains(text, "reviewer run started") || !strings.Contains(text, "`review-test`") || !strings.Contains(text, "`#12`") {
		t.Fatalf("Handle(review pr) text = %q", text)
	}
	if runner.startedReviewRunID != "review-test" || runner.reviewPRNumber != 12 {
		t.Fatalf("runner = %#v", runner)
	}
	if runner.reviewCodexSecret != "matter-codex-codex-auth-primary" {
		t.Fatalf("reviewCodexSecret = %q", runner.reviewCodexSecret)
	}
	if runner.reviewGitHubSecret != "matter-codex-github" {
		t.Fatalf("reviewGitHubSecret = %q", runner.reviewGitHubSecret)
	}
	if store.agentRun.RunID != "review-test" || store.agentRun.ProfileName != "reviewer" || store.agentRun.HeadBranch != "pr-12" {
		t.Fatalf("agentRun = %#v", store.agentRun)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "review status review-test"})
	if !strings.Contains(text, "reviewer run status") || !strings.Contains(text, "review-decision") || !strings.Contains(text, "comment") {
		t.Fatalf("Handle(review status) text = %q", text)
	}
	if store.updatedRunStatus != "review_comment" || store.updatedPRURL != "https://github.example/pr/12" {
		t.Fatalf("updated run = %q %q", store.updatedRunStatus, store.updatedPRURL)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "review cleanup review-test", UserName: "owner"})
	if !strings.Contains(text, "reviewer run cleanup") || !strings.Contains(text, "pvc deleted: `true`") {
		t.Fatalf("Handle(review cleanup) text = %q", text)
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
	developerCodexSecret  string
	developerGitHubSecret string
	startedReviewRunID    string
	reviewPRNumber        int
	reviewCodexSecret     string
	reviewGitHubSecret    string
	cleanedRunID          string
	authAccount           string
	authSecret            string
	authReady             bool
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

func (runner *fakeRuntimeRunner) StartCodexAuthSession(_ context.Context, input runtimerepo.CodexAuthSessionInput) (runtimerepo.CodexAuthSession, error) {
	runner.authAccount = input.AccountName
	runner.authSecret = input.SecretName
	return runtimerepo.CodexAuthSession{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		JobName:     "mc-codex-auth-" + input.AccountName,
		Created:     true,
	}, nil
}

func (runner *fakeRuntimeRunner) GetCodexAuthStatus(_ context.Context, accountName string, secretName string) (runtimerepo.CodexAuthStatus, error) {
	return runtimerepo.CodexAuthStatus{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   "mattermost",
		JobName:     "mc-codex-auth-" + accountName,
		PodName:     "mc-codex-auth-" + accountName + "-pod",
		Exists:      true,
		JobActive:   1,
		PodPhase:    "Running",
		DeviceURL:   "https://auth.openai.com/codex/device",
		DeviceCode:  "ABCD-12345",
		AuthReady:   runner.authReady,
	}, nil
}

func (runner *fakeRuntimeRunner) CompleteCodexAuthSession(_ context.Context, input runtimerepo.CodexAuthCompleteInput) (runtimerepo.CodexAuthCompleteResult, error) {
	return runtimerepo.CodexAuthCompleteResult{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		Saved:       true,
	}, nil
}

func (runner *fakeRuntimeRunner) CleanupCodexAuthSession(_ context.Context, accountName string) (runtimerepo.CodexAuthCleanupResult, error) {
	return runtimerepo.CodexAuthCleanupResult{
		AccountName: accountName,
		Namespace:   "mattermost",
		JobDeleted:  true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartDeveloperRun(_ context.Context, input runtimerepo.DeveloperRunInput) (runtimerepo.StartedRun, error) {
	runner.startedDeveloperRunID = input.RunID
	runner.developerHeadBranch = input.HeadBranch
	runner.developerCodexSecret = input.CodexAuthSecretName
	runner.developerGitHubSecret = input.GitHubSecretName
	if strings.TrimSpace(input.Prompt) == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartReviewRun(_ context.Context, input runtimerepo.ReviewRunInput) (runtimerepo.StartedRun, error) {
	runner.startedReviewRunID = input.RunID
	runner.reviewPRNumber = input.PRNumber
	runner.reviewCodexSecret = input.CodexAuthSecretName
	runner.reviewGitHubSecret = input.GitHubSecretName
	if strings.TrimSpace(input.Prompt) == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) GetRunStatus(_ context.Context, runID string) (runtimerepo.RunStatus, error) {
	artifacts := map[string]string{"pr-url": "https://github.example/pr/10"}
	logTail := "matter-codex smoke done\nmatter-codex artifact pr-url: https://github.example/pr/10\nsmoke-ok"
	if strings.HasPrefix(runID, "review-") {
		artifacts = map[string]string{
			"pr-url":           "https://github.example/pr/12",
			"review-decision":  "comment",
			"review-submitted": "true",
		}
		logTail = "matter-codex reviewer run done\nmatter-codex artifact pr-url: https://github.example/pr/12\nmatter-codex artifact review-decision: comment\nmatter-codex artifact review-submitted: true"
	}
	return runtimerepo.RunStatus{
		RunID:        runID,
		Namespace:    "mattermost",
		JobName:      "mc-run-" + runID,
		PVCName:      "mc-ws-" + runID,
		PodName:      "mc-run-" + runID + "-pod",
		Exists:       true,
		JobSucceeded: 1,
		PodPhase:     "Succeeded",
		LogTail:      logTail,
		Artifacts:    artifacts,
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
	openAIAccounts   map[string]entity.OpenAIAccount
	githubAccounts   map[string]entity.GitHubAccount
	promptTemplates  map[string]entity.AgentPromptTemplate
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

func (store *fakeAdminStore) ListAgentPromptTemplates(_ context.Context, profileName string) ([]entity.AgentPromptTemplate, error) {
	store.ensurePromptTemplates()
	var items []entity.AgentPromptTemplate
	for _, item := range store.promptTemplates {
		if profileName == "" || item.ProfileName == profileName {
			items = append(items, item)
		}
	}
	return items, nil
}

func (store *fakeAdminStore) GetAgentPromptTemplate(_ context.Context, profileName string, templateKey string) (entity.AgentPromptTemplate, error) {
	store.ensurePromptTemplates()
	item, ok := store.promptTemplates[promptTemplateMapKey(profileName, templateKey)]
	if !ok {
		return entity.AgentPromptTemplate{}, fmt.Errorf("prompt template not found")
	}
	return item, nil
}

func (store *fakeAdminStore) UpsertAgentPromptTemplate(_ context.Context, input adminrepo.UpsertAgentPromptTemplateInput) (entity.AgentPromptTemplate, bool, error) {
	store.ensurePromptTemplates()
	key := promptTemplateMapKey(input.ProfileName, input.TemplateKey)
	_, exists := store.promptTemplates[key]
	item := entity.AgentPromptTemplate{
		ID:          1,
		ProfileName: input.ProfileName,
		TemplateKey: input.TemplateKey,
		Body:        input.Body,
	}
	store.promptTemplates[key] = item
	return item, !exists, nil
}

func (store *fakeAdminStore) ensurePromptTemplates() {
	if store.promptTemplates != nil {
		return
	}
	store.promptTemplates = map[string]entity.AgentPromptTemplate{
		promptTemplateMapKey("developer", developerSmokeTemplateKey): {
			ID:          1,
			ProfileName: "developer",
			TemplateKey: developerSmokeTemplateKey,
			Body:        "Developer task for {{.Repository.FullName}}: {{.Task.Body}}",
		},
		promptTemplateMapKey("reviewer", reviewPRTemplateKey): {
			ID:          2,
			ProfileName: "reviewer",
			TemplateKey: reviewPRTemplateKey,
			Body:        "DECISION: comment\nReview {{.Repository.FullName}} PR #{{.PullRequest.Number}}",
		},
	}
}

func promptTemplateMapKey(profileName string, templateKey string) string {
	return profileName + "/" + templateKey
}

func (store *fakeAdminStore) UpsertOpenAIAccount(_ context.Context, input adminrepo.UpsertOpenAIAccountInput) (entity.OpenAIAccount, bool, error) {
	if store.openAIAccounts == nil {
		store.openAIAccounts = make(map[string]entity.OpenAIAccount)
	}
	_, exists := store.openAIAccounts[input.Name]
	account := entity.OpenAIAccount{
		ID:        1,
		Name:      input.Name,
		SecretRef: input.SecretRef,
		Status:    input.Status,
	}
	store.openAIAccounts[input.Name] = account
	return account, !exists, nil
}

func (store *fakeAdminStore) ListOpenAIAccounts(context.Context, int) ([]entity.OpenAIAccount, error) {
	accounts := make([]entity.OpenAIAccount, 0, len(store.openAIAccounts))
	for _, account := range store.openAIAccounts {
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (store *fakeAdminStore) GetOpenAIAccount(_ context.Context, name string) (entity.OpenAIAccount, error) {
	if account, ok := store.openAIAccounts[name]; ok {
		return account, nil
	}
	return entity.OpenAIAccount{}, fmt.Errorf("openai account not found")
}

func (store *fakeAdminStore) GetGitHubAccount(_ context.Context, name string) (entity.GitHubAccount, error) {
	store.ensureGitHubAccounts()
	if account, ok := store.githubAccounts[name]; ok {
		return account, nil
	}
	return entity.GitHubAccount{}, fmt.Errorf("github account not found")
}

func (store *fakeAdminStore) ensureGitHubAccounts() {
	if store.githubAccounts != nil {
		return
	}
	store.githubAccounts = map[string]entity.GitHubAccount{
		"primary": {Name: "primary", SecretRef: "matter-codex-github", Status: "configured"},
		"agent":   {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"},
	}
}

func (store *fakeAdminStore) UpdateOpenAIAccountStatus(_ context.Context, input adminrepo.UpdateOpenAIAccountStatusInput) (entity.OpenAIAccount, error) {
	if store.openAIAccounts == nil {
		store.openAIAccounts = make(map[string]entity.OpenAIAccount)
	}
	account := store.openAIAccounts[input.Name]
	account.Name = input.Name
	if input.SecretRef != "" {
		account.SecretRef = input.SecretRef
	}
	account.Status = input.Status
	store.openAIAccounts[input.Name] = account
	return account, nil
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
