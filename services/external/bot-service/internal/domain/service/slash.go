package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

type MattermostChannelManager interface {
	EnsureRepositoryChannel(ctx context.Context, teamName string, channelName string, displayName string) (bool, error)
}

type FlowCardPublisher interface {
	UpsertFlowCard(ctx context.Context, card FlowCard) (FlowCardPost, error)
}

type FlowCard struct {
	ChannelID string
	PostID    string
	ActionURL string
	Message   string
	Color     string
	Title     string
	Text      string
	Fields    []FlowCardField
	Actions   []FlowCardAction
}

type FlowCardField struct {
	Title string
	Value string
	Short bool
}

type FlowCardAction struct {
	ID       string
	Name     string
	Tooltip  string
	Style    string
	Disabled bool
	Context  map[string]any
}

type FlowCardPost struct {
	ChannelID string
	PostID    string
}

type SlashCommand struct {
	Text        string
	UserID      string
	UserName    string
	ChannelID   string
	ChannelName string
	TeamID      string
	TeamDomain  string
}

type FlowActionCommand struct {
	FlowID    string
	Action    string
	Token     string
	UserID    string
	UserName  string
	ChannelID string
	PostID    string
}

type FlowActionResult struct {
	EphemeralText string
	StatusCode    int
}

type SlashCommandServiceConfig struct {
	Localizer               *texti18n.Localizer
	StatusService           *StatusService
	Store                   adminrepo.Repository
	ChannelManager          MattermostChannelManager
	FlowCardPublisher       FlowCardPublisher
	RepositoryProvider      providerrepo.RepositoryProvider
	RuntimeRunner           runtimerepo.Runner
	DefaultTeamName         string
	CodexAuthSecretName     string
	FlowActionURL           string
	BotTokenConfigured      bool
	SlashTokenConfigured    bool
	GitHubTokenConfigured   bool
	GitHubWebhookConfigured bool
	DatabaseConfigured      bool
	StorageReady            bool
	RuntimeConfigured       bool
	MattermostConfigured    bool
	ChannelManagerEnabled   bool
}

type SlashCommandService struct {
	cfg SlashCommandServiceConfig
}

func NewSlashCommandService(cfg SlashCommandServiceConfig) *SlashCommandService {
	return &SlashCommandService{cfg: cfg}
}

func (svc *SlashCommandService) Handle(ctx context.Context, command SlashCommand) string {
	fields := strings.Fields(command.Text)
	if len(fields) == 0 || fields[0] == "status" {
		return svc.cfg.StatusService.SlashStatusText()
	}

	switch fields[0] {
	case "help":
		return svc.helpText()
	case "repo":
		return svc.handleRepo(ctx, fields[1:], command)
	case "token":
		return svc.handleToken(ctx, fields[1:])
	case "locale":
		return svc.handleLocale(fields[1:])
	case "runtime":
		return svc.handleRuntime(ctx, fields[1:], command)
	case "dev":
		return svc.handleDev(ctx, fields[1:], command)
	case "review":
		return svc.handleReview(ctx, fields[1:], command)
	case "flow":
		return svc.handleFlow(ctx, fields[1:], command)
	case "prompt":
		return svc.handlePrompt(ctx, strings.TrimSpace(strings.TrimPrefix(command.Text, fields[0])), command)
	case "profile":
		return svc.handleProfile(ctx, fields[1:])
	case "openai":
		return svc.handleOpenAI(ctx, fields[1:], command)
	case "github":
		return svc.handleGitHub(ctx, fields[1:], command)
	default:
		return svc.t("slash.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleRepo(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return svc.t("repo.usage", nil)
	}
	switch args[0] {
	case "add":
		return svc.handleRepoAdd(ctx, args[1:], command)
	case "list":
		return svc.handleRepoList(ctx)
	default:
		return svc.t("repo.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleRepoAdd(ctx context.Context, args []string, command SlashCommand) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("repo.add.storage_not_ready", nil)
	}
	input, err := parseRepoAdd(args)
	if err != nil {
		return svc.validationErrorText(err)
	}
	channelName := repositoryChannelName(input.Owner, input.Name)
	if svc.cfg.ChannelManager != nil {
		if _, err := svc.cfg.ChannelManager.EnsureRepositoryChannel(ctx, svc.cfg.DefaultTeamName, channelName, "repo "+input.Owner+"/"+input.Name); err != nil {
			return svc.t("repo.add.channel_failed", map[string]any{"Error": safeError(err)})
		}
	}
	input.MattermostChannel = channelName
	repo, created, err := svc.cfg.Store.UpsertRepository(ctx, input)
	if err != nil {
		return svc.t("repo.add.save_failed", map[string]any{"Error": safeError(err)})
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "repository.upserted",
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "repository",
		ResourceName: repo.Provider + ":" + repo.FullName(),
		Summary:      "repository metadata upserted from Mattermost slash command",
	})
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	lines := []string{
		svc.t("repo.add.result", map[string]any{
			"State":         svc.t(stateID, nil),
			"Provider":      repo.Provider,
			"FullName":      repo.FullName(),
			"Channel":       repo.MattermostChannel,
			"DefaultBranch": repo.DefaultBranch,
		}),
	}
	lines = append(lines, svc.ensureRepositoryWebhookLine(ctx, command, repo.Provider, repo.Owner, repo.Name)...)
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleRepoList(ctx context.Context) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("repo.list.storage_not_ready", nil)
	}
	repositories, err := svc.cfg.Store.ListRepositories(ctx, 20)
	if err != nil {
		return svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})
	}
	if len(repositories) == 0 {
		return svc.t("repo.list.empty", nil)
	}
	lines := []string{svc.t("repo.list.header", nil)}
	for _, repo := range repositories {
		lines = append(lines, svc.t("repo.list.item", map[string]any{
			"Provider":      repo.Provider,
			"FullName":      repo.FullName(),
			"DefaultBranch": repo.DefaultBranch,
			"Channel":       repo.MattermostChannel,
			"Status":        repo.Status,
		}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleToken(ctx context.Context, args []string) string {
	if len(args) != 1 || args[0] != "check" {
		return svc.t("token.usage", nil)
	}
	lines := []string{
		svc.t("token.check.header", nil),
		svc.t("token.check.mattermost_bot", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.BotTokenConfigured)}),
		svc.t("token.check.mattermost_slash", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.SlashTokenConfigured)}),
		svc.t("token.check.github", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.GitHubTokenConfigured)}),
		svc.t("token.check.github_webhook", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.GitHubWebhookConfigured)}),
		svc.t("token.check.database", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.DatabaseConfigured)}),
		svc.t("token.check.storage", map[string]any{"Status": readyLabel(svc.cfg.Localizer, svc.cfg.StorageReady)}),
		svc.t("token.check.runtime", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.RuntimeConfigured)}),
		svc.t("token.check.channel_manager", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.ChannelManagerEnabled)}),
	}
	if svc.cfg.StorageReady && svc.cfg.Store != nil {
		accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
		if err == nil {
			authorized := 0
			for _, account := range accounts {
				if account.Status == "authorized" {
					authorized++
				}
			}
			lines = append(lines, svc.t("token.check.openai_accounts", map[string]any{"Authorized": authorized, "Total": len(accounts)}))
		}
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleRuntime(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) == 0 {
		return svc.t("runtime.usage", nil)
	}
	switch args[0] {
	case "smoke":
		return svc.handleRuntimeSmoke(ctx, args[1:], command)
	case "status":
		return svc.handleRuntimeStatus(ctx, args[1:])
	case "cleanup":
		return svc.handleRuntimeCleanup(ctx, args[1:], command)
	default:
		return svc.t("runtime.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleRuntimeSmoke(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) > 1 {
		return svc.t("runtime.smoke.usage", nil)
	}
	runID := defaultRuntimeRunID()
	if len(args) == 1 {
		runID = args[0]
	}
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	started, err := svc.cfg.RuntimeRunner.StartSmokeRun(ctx, runtimerepo.SmokeRunInput{RunID: runID, Role: "smoke"})
	if err != nil {
		return svc.t("runtime.smoke.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordRuntimeAudit(ctx, command, "runtime.smoke_run.started", started.RunID, "kubernetes smoke runner job started from Mattermost slash command")
	return svc.t("runtime.smoke.started", map[string]any{
		"RunID":     started.RunID,
		"Namespace": started.Namespace,
		"Job":       started.JobName,
		"PVC":       started.PVCName,
		"Created":   started.Created,
	})
}

func (svc *SlashCommandService) handleRuntimeStatus(ctx context.Context, args []string) string {
	if len(args) != 1 {
		return svc.t("runtime.status.usage", nil)
	}
	runID := args[0]
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	status, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, runID)
	if err != nil {
		return svc.t("runtime.status.failed", map[string]any{"Error": safeError(err)})
	}
	if !status.Exists {
		return svc.t("runtime.status.not_found", map[string]any{"RunID": runID})
	}
	lines := []string{
		svc.t("runtime.status.header", nil),
		svc.t("runtime.status.identity", map[string]any{
			"RunID":     status.RunID,
			"Namespace": status.Namespace,
			"Job":       status.JobName,
			"PVC":       status.PVCName,
		}),
		svc.t("runtime.status.job", map[string]any{
			"Active":    status.JobActive,
			"Succeeded": status.JobSucceeded,
			"Failed":    status.JobFailed,
		}),
	}
	if status.PodName != "" {
		lines = append(lines, svc.t("runtime.status.pod", map[string]any{
			"Pod":   status.PodName,
			"Phase": emptyAsUnknown(status.PodPhase),
		}))
	}
	if strings.TrimSpace(status.LogTail) != "" {
		lines = append(lines, svc.t("runtime.status.logs", map[string]any{"Logs": sanitizeLogTail(status.LogTail)}))
	}
	for _, artifact := range sortedArtifacts(status.Artifacts) {
		lines = append(lines, svc.t("runtime.status.artifact", map[string]any{"Key": artifact.key, "Value": artifact.value}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleRuntimeCleanup(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) != 1 {
		return svc.t("runtime.cleanup.usage", nil)
	}
	runID := args[0]
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	result, err := svc.cfg.RuntimeRunner.CleanupRun(ctx, runID)
	if err != nil {
		return svc.t("runtime.cleanup.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordRuntimeAudit(ctx, command, "runtime.run.cleaned", result.RunID, "kubernetes smoke runner resources cleaned from Mattermost slash command")
	return svc.t("runtime.cleanup.result", map[string]any{
		"RunID":      result.RunID,
		"Namespace":  result.Namespace,
		"JobDeleted": result.JobDeleted,
		"PVCDeleted": result.PVCDeleted,
	})
}

func (svc *SlashCommandService) handleDev(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) == 0 {
		return svc.t("dev.usage", nil)
	}
	switch args[0] {
	case "smoke":
		return svc.handleDevSmoke(ctx, args[1:], command)
	case "status":
		return svc.handleDevStatus(ctx, args[1:])
	case "cleanup":
		return svc.handleDevCleanup(ctx, args[1:], command)
	default:
		return svc.t("dev.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleDevSmoke(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) < 1 || len(args) > 2 {
		return svc.t("dev.smoke.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("dev.storage_not_ready", nil)
	}
	profile, ok := svc.agentProfile(ctx, "developer")
	if !ok || !profile.Enabled {
		return svc.t("dev.profile_not_ready", map[string]any{"Profile": "developer"})
	}
	accountName := defaultString(profile.OpenAIAccountName, "primary")
	account, ok := svc.openAIAccount(ctx, accountName)
	if !ok || account.Status != "authorized" || strings.TrimSpace(account.SecretRef) == "" {
		return svc.t("dev.openai_account_not_ready", map[string]any{"Account": accountName})
	}
	githubAccountName := defaultString(profile.GitHubAccountName, "primary")
	githubAccount, ok := svc.githubAccount(ctx, githubAccountName)
	if !ok || strings.TrimSpace(githubAccount.SecretRef) == "" {
		return svc.t("dev.github_account_not_ready", map[string]any{"Account": githubAccountName})
	}
	ref, err := parseRepositoryRef(args[:1], "dev smoke")
	if err != nil {
		return svc.validationErrorText(err)
	}
	runID := defaultDeveloperRunID()
	if len(args) == 2 {
		runID = args[1]
	}
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	headBranch := developerSmokeBranch(runID)
	task := developerSmokeTask(runID, ref)
	prompt, err := svc.renderStoredPromptTemplate(ctx, "developer", developerSmokeTemplateKey, promptTemplateData{
		Run: promptTemplateRunData{
			ID:      runID,
			Profile: "developer",
			Role:    "developer",
			Locale:  svc.cfg.Localizer.Locale(),
		},
		Agent: promptTemplateAgentData{
			Profile: "developer",
			Role:    "developer",
		},
		Repository: promptTemplateRepositoryData{
			Provider: "github",
			Owner:    ref.Owner,
			Name:     ref.Name,
			FullName: ref.Owner + "/" + ref.Name,
		},
		Task: promptTemplateTaskData{
			Title:      "Matter Codex developer smoke " + runID,
			Body:       task,
			BaseBranch: "main",
			HeadBranch: headBranch,
		},
		GitHub: promptGitHubData(githubAccountName),
		Locale: svc.promptTemplateLocaleData(),
	})
	if err != nil {
		return svc.t("prompt.render.failed", map[string]any{"Error": safeError(err)})
	}
	started, err := svc.cfg.RuntimeRunner.StartDeveloperRun(ctx, runtimerepo.DeveloperRunInput{
		RunID:               runID,
		Profile:             "developer",
		CodexAuthSecretName: account.SecretRef,
		GitHubSecretName:    githubAccount.SecretRef,
		Provider:            "github",
		Owner:               ref.Owner,
		Name:                ref.Name,
		BaseBranch:          "main",
		HeadBranch:          headBranch,
		Title:               "Matter Codex developer smoke " + runID,
		Task:                task,
		Prompt:              prompt,
	})
	if err != nil {
		return svc.t("dev.smoke.failed", map[string]any{"Error": safeError(err)})
	}
	if _, err := svc.cfg.Store.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID:               runID,
		ProfileName:         "developer",
		Role:                "developer",
		Provider:            "github",
		Owner:               ref.Owner,
		Name:                ref.Name,
		BaseBranch:          "main",
		HeadBranch:          headBranch,
		Status:              "started",
		KubernetesNamespace: started.Namespace,
		JobName:             started.JobName,
		PVCName:             started.PVCName,
		Summary:             "developer smoke run started from Mattermost slash command",
	}); err != nil {
		return svc.t("dev.smoke.store_failed", map[string]any{"RunID": runID, "Error": safeError(err)})
	}
	svc.recordRuntimeAudit(ctx, command, "developer.run.started", started.RunID, "developer smoke runner job started from Mattermost slash command")
	return svc.t("dev.smoke.started", map[string]any{
		"RunID":      started.RunID,
		"Repository": ref.Owner + "/" + ref.Name,
		"Branch":     headBranch,
		"Account":    account.Name,
		"Namespace":  started.Namespace,
		"Job":        started.JobName,
		"PVC":        started.PVCName,
	})
}

func (svc *SlashCommandService) handleDevStatus(ctx context.Context, args []string) string {
	if len(args) != 1 {
		return svc.t("dev.status.usage", nil)
	}
	runID := args[0]
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	run, hasRun := svc.agentRun(ctx, runID)
	status, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, runID)
	if err != nil {
		return svc.t("dev.status.failed", map[string]any{"Error": safeError(err)})
	}
	if !status.Exists {
		return svc.t("runtime.status.not_found", map[string]any{"RunID": runID})
	}
	derivedStatus := developerRunStatus(status)
	if hasRun {
		prURL := status.Artifacts["pr-url"]
		if updated, err := svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: runID, Status: derivedStatus, PRURL: prURL}); err == nil {
			run = updated
		}
	}
	lines := []string{svc.t("dev.status.header", nil)}
	if hasRun {
		lines = append(lines, svc.t("dev.status.run", map[string]any{
			"RunID":      run.RunID,
			"Profile":    run.ProfileName,
			"Repository": run.FullName(),
			"Base":       run.BaseBranch,
			"Branch":     run.HeadBranch,
			"Status":     run.Status,
		}))
	} else {
		lines = append(lines, svc.t("dev.status.run_without_storage", map[string]any{"RunID": runID, "Status": derivedStatus}))
	}
	lines = append(lines,
		svc.t("runtime.status.identity", map[string]any{
			"RunID":     status.RunID,
			"Namespace": status.Namespace,
			"Job":       status.JobName,
			"PVC":       status.PVCName,
		}),
		svc.t("runtime.status.job", map[string]any{
			"Active":    status.JobActive,
			"Succeeded": status.JobSucceeded,
			"Failed":    status.JobFailed,
		}),
	)
	if status.PodName != "" {
		lines = append(lines, svc.t("runtime.status.pod", map[string]any{
			"Pod":   status.PodName,
			"Phase": emptyAsUnknown(status.PodPhase),
		}))
	}
	for _, artifact := range sortedArtifacts(status.Artifacts) {
		lines = append(lines, svc.t("dev.status.artifact", map[string]any{"Key": artifact.key, "Value": artifact.value}))
	}
	if strings.TrimSpace(status.LogTail) != "" {
		lines = append(lines, svc.t("runtime.status.logs", map[string]any{"Logs": sanitizeLogTail(status.LogTail)}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleDevCleanup(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) != 1 {
		return svc.t("dev.cleanup.usage", nil)
	}
	runID := args[0]
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	result, err := svc.cfg.RuntimeRunner.CleanupRun(ctx, runID)
	if err != nil {
		return svc.t("dev.cleanup.failed", map[string]any{"Error": safeError(err)})
	}
	if svc.cfg.StorageReady && svc.cfg.Store != nil {
		_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: runID, Status: "cleaned"})
	}
	svc.recordRuntimeAudit(ctx, command, "developer.run.cleaned", result.RunID, "developer runner resources cleaned from Mattermost slash command")
	return svc.t("dev.cleanup.result", map[string]any{
		"RunID":      result.RunID,
		"Namespace":  result.Namespace,
		"JobDeleted": result.JobDeleted,
		"PVCDeleted": result.PVCDeleted,
	})
}

func (svc *SlashCommandService) handleReview(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) == 0 {
		return svc.t("review.usage", nil)
	}
	switch args[0] {
	case "pr":
		return svc.handleReviewPR(ctx, args[1:], command)
	case "status":
		return svc.handleReviewStatus(ctx, args[1:])
	case "cleanup":
		return svc.handleReviewCleanup(ctx, args[1:], command)
	default:
		return svc.t("review.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleReviewPR(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) < 2 || len(args) > 3 {
		return svc.t("review.pr.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("review.storage_not_ready", nil)
	}
	profile, ok := svc.agentProfile(ctx, "reviewer")
	if !ok || !profile.Enabled {
		return svc.t("review.profile_not_ready", map[string]any{"Profile": "reviewer"})
	}
	accountName := defaultString(profile.OpenAIAccountName, "primary")
	account, ok := svc.openAIAccount(ctx, accountName)
	if !ok || account.Status != "authorized" || strings.TrimSpace(account.SecretRef) == "" {
		return svc.t("review.openai_account_not_ready", map[string]any{"Account": accountName})
	}
	githubAccountName := defaultString(profile.GitHubAccountName, "primary")
	githubAccount, ok := svc.githubAccount(ctx, githubAccountName)
	if !ok || strings.TrimSpace(githubAccount.SecretRef) == "" {
		return svc.t("review.github_account_not_ready", map[string]any{"Account": githubAccountName})
	}
	ref, err := parseRepositoryRef(args[:1], "review pr")
	if err != nil {
		return svc.validationErrorText(err)
	}
	number, err := strconv.Atoi(args[1])
	if err != nil || number <= 0 {
		return svc.t("review.pr.invalid_number", nil)
	}
	runID := defaultReviewRunID()
	if len(args) == 3 {
		runID = args[2]
	}
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	prompt, err := svc.renderStoredPromptTemplate(ctx, "reviewer", reviewPRTemplateKey, promptTemplateData{
		Run: promptTemplateRunData{
			ID:      runID,
			Profile: "reviewer",
			Role:    "reviewer",
			Locale:  svc.cfg.Localizer.Locale(),
		},
		Agent: promptTemplateAgentData{
			Profile: "reviewer",
			Role:    "reviewer",
		},
		Repository: promptTemplateRepositoryData{
			Provider: "github",
			Owner:    ref.Owner,
			Name:     ref.Name,
			FullName: ref.Owner + "/" + ref.Name,
		},
		PullRequest: promptTemplatePullRequestData{
			Number: number,
		},
		GitHub: promptGitHubData(githubAccountName),
		Locale: svc.promptTemplateLocaleData(),
	})
	if err != nil {
		return svc.t("prompt.render.failed", map[string]any{"Error": safeError(err)})
	}
	started, err := svc.cfg.RuntimeRunner.StartReviewRun(ctx, runtimerepo.ReviewRunInput{
		RunID:               runID,
		Profile:             "reviewer",
		CodexAuthSecretName: account.SecretRef,
		GitHubSecretName:    githubAccount.SecretRef,
		Provider:            "github",
		Owner:               ref.Owner,
		Name:                ref.Name,
		PRNumber:            number,
		Prompt:              prompt,
	})
	if err != nil {
		return svc.t("review.pr.failed", map[string]any{"Error": safeError(err)})
	}
	if _, err := svc.cfg.Store.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID:               runID,
		ProfileName:         "reviewer",
		Role:                "reviewer",
		Provider:            "github",
		Owner:               ref.Owner,
		Name:                ref.Name,
		BaseBranch:          "pr",
		HeadBranch:          fmt.Sprintf("pr-%d", number),
		Status:              "started",
		KubernetesNamespace: started.Namespace,
		JobName:             started.JobName,
		PVCName:             started.PVCName,
		Summary:             "review run started from Mattermost slash command",
	}); err != nil {
		return svc.t("review.pr.store_failed", map[string]any{"RunID": runID, "Error": safeError(err)})
	}
	svc.recordRuntimeAudit(ctx, command, "reviewer.run.started", started.RunID, "reviewer runner job started from Mattermost slash command")
	return svc.t("review.pr.started", map[string]any{
		"RunID":      started.RunID,
		"Repository": ref.Owner + "/" + ref.Name,
		"PR":         number,
		"Account":    account.Name,
		"Namespace":  started.Namespace,
		"Job":        started.JobName,
		"PVC":        started.PVCName,
	})
}

func (svc *SlashCommandService) handleReviewStatus(ctx context.Context, args []string) string {
	if len(args) != 1 {
		return svc.t("review.status.usage", nil)
	}
	runID := args[0]
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	run, hasRun := svc.agentRun(ctx, runID)
	status, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, runID)
	if err != nil {
		return svc.t("review.status.failed", map[string]any{"Error": safeError(err)})
	}
	if !status.Exists {
		return svc.t("runtime.status.not_found", map[string]any{"RunID": runID})
	}
	derivedStatus := reviewerRunStatus(status)
	if hasRun {
		prURL := status.Artifacts["pr-url"]
		if updated, err := svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: runID, Status: derivedStatus, PRURL: prURL}); err == nil {
			run = updated
		}
	}
	lines := []string{svc.t("review.status.header", nil)}
	if hasRun {
		lines = append(lines, svc.t("review.status.run", map[string]any{
			"RunID":      run.RunID,
			"Profile":    run.ProfileName,
			"Repository": run.FullName(),
			"Base":       run.BaseBranch,
			"Branch":     run.HeadBranch,
			"Status":     run.Status,
		}))
	} else {
		lines = append(lines, svc.t("review.status.run_without_storage", map[string]any{"RunID": runID, "Status": derivedStatus}))
	}
	lines = append(lines,
		svc.t("runtime.status.identity", map[string]any{
			"RunID":     status.RunID,
			"Namespace": status.Namespace,
			"Job":       status.JobName,
			"PVC":       status.PVCName,
		}),
		svc.t("runtime.status.job", map[string]any{
			"Active":    status.JobActive,
			"Succeeded": status.JobSucceeded,
			"Failed":    status.JobFailed,
		}),
	)
	if status.PodName != "" {
		lines = append(lines, svc.t("runtime.status.pod", map[string]any{
			"Pod":   status.PodName,
			"Phase": emptyAsUnknown(status.PodPhase),
		}))
	}
	for _, artifact := range sortedArtifacts(status.Artifacts) {
		lines = append(lines, svc.t("review.status.artifact", map[string]any{"Key": artifact.key, "Value": artifact.value}))
	}
	if strings.TrimSpace(status.LogTail) != "" {
		lines = append(lines, svc.t("runtime.status.logs", map[string]any{"Logs": sanitizeLogTail(status.LogTail)}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleReviewCleanup(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) != 1 {
		return svc.t("review.cleanup.usage", nil)
	}
	runID := args[0]
	if !validRuntimeRunID(runID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	result, err := svc.cfg.RuntimeRunner.CleanupRun(ctx, runID)
	if err != nil {
		return svc.t("review.cleanup.failed", map[string]any{"Error": safeError(err)})
	}
	if svc.cfg.StorageReady && svc.cfg.Store != nil {
		_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: runID, Status: "cleaned"})
	}
	svc.recordRuntimeAudit(ctx, command, "reviewer.run.cleaned", result.RunID, "reviewer runner resources cleaned from Mattermost slash command")
	return svc.t("review.cleanup.result", map[string]any{
		"RunID":      result.RunID,
		"Namespace":  result.Namespace,
		"JobDeleted": result.JobDeleted,
		"PVCDeleted": result.PVCDeleted,
	})
}

func (svc *SlashCommandService) handleFlow(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return svc.t("flow.usage", nil)
	}
	switch args[0] {
	case "start":
		return svc.handleFlowStart(ctx, args[1:], command)
	case "status":
		return svc.handleFlowStatus(ctx, args[1:])
	case "card":
		return svc.handleFlowCard(ctx, args[1:], command)
	case "cleanup":
		return svc.handleFlowCleanup(ctx, args[1:], command)
	default:
		return svc.t("flow.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleFlowStart(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) < 3 {
		return svc.t("flow.start.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("flow.storage_not_ready", nil)
	}
	ref, err := parseRepositoryRef(args[:1], "flow start")
	if err != nil {
		return svc.validationErrorText(err)
	}
	flowID := strings.ToLower(strings.TrimSpace(args[1]))
	if !validFlowID(flowID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	title := strings.TrimSpace(strings.Join(args[2:], " "))
	if title == "" {
		return svc.t("flow.start.usage", nil)
	}
	if _, message, ok := svc.flowAgentRuntime(ctx, "developer"); !ok {
		return message
	}
	if _, message, ok := svc.flowAgentRuntime(ctx, "reviewer"); !ok {
		return message
	}
	flow, created, err := svc.cfg.Store.CreateAgentFlow(ctx, adminrepo.CreateAgentFlowInput{
		FlowID:      flowID,
		Status:      flowStatusCreated,
		Provider:    "github",
		Owner:       ref.Owner,
		Name:        ref.Name,
		BaseBranch:  "main",
		HeadBranch:  flowHeadBranch(flowID),
		Title:       title,
		Task:        title,
		Attempt:     1,
		MaxAttempts: defaultFlowMaxAttempts,
		OwnerUserID: strings.TrimSpace(command.UserID),
		OwnerUser:   strings.TrimSpace(command.UserName),
		ActionToken: newFlowActionToken(),
		Summary:     "developer-review flow created from Mattermost slash command",
	})
	if err != nil {
		return svc.t("flow.start.store_failed", map[string]any{"Error": safeError(err)})
	}
	if !created {
		return svc.t("flow.start.exists", map[string]any{"FlowID": flow.FlowID, "Status": flow.Status})
	}
	flow, started, err := svc.startFlowDeveloperAttempt(ctx, flow, false)
	if err != nil {
		_, _ = svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: safeError(err)})
		return svc.t("flow.start.failed", map[string]any{"Error": safeError(err)})
	}
	cardLine := svc.t("flow.start.card_skipped", nil)
	if command.ChannelID != "" && svc.cfg.FlowCardPublisher != nil && svc.cfg.FlowActionURL != "" {
		updated, post, err := svc.publishFlowCard(ctx, flow, command)
		if err != nil {
			cardLine = svc.t("flow.start.card_failed", map[string]any{"Error": safeError(err)})
		} else {
			flow = updated
			cardLine = svc.t("flow.start.card_posted", map[string]any{"PostID": post.PostID})
		}
	}
	svc.recordFlowAudit(ctx, command, "flow.started", flow.FlowID, "developer-review flow started from Mattermost slash command")
	return svc.t("flow.start.started", map[string]any{
		"FlowID":      flow.FlowID,
		"Repository":  flow.FullName(),
		"Branch":      flow.HeadBranch,
		"Attempt":     flow.Attempt,
		"MaxAttempts": flow.MaxAttempts,
		"RunID":       started.RunID,
		"Namespace":   started.Namespace,
		"Job":         started.JobName,
		"PVC":         started.PVCName,
		"Card":        cardLine,
	})
}

func (svc *SlashCommandService) handleFlowStatus(ctx context.Context, args []string) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) != 1 {
		return svc.t("flow.status.usage", nil)
	}
	flowID := strings.ToLower(strings.TrimSpace(args[0]))
	if !validFlowID(flowID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("flow.storage_not_ready", nil)
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, flowID)
	if err != nil {
		return svc.t("flow.status.failed", map[string]any{"Error": safeError(err)})
	}
	flow, events := svc.advanceFlow(ctx, flow)
	return svc.flowStatusText(flow, events)
}

func (svc *SlashCommandService) handleFlowCard(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) != 1 {
		return svc.t("flow.card.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("flow.storage_not_ready", nil)
	}
	flowID := strings.ToLower(strings.TrimSpace(args[0]))
	if !validFlowID(flowID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, flowID)
	if err != nil {
		return svc.t("flow.card.failed", map[string]any{"Error": safeError(err)})
	}
	flow, post, err := svc.publishFlowCard(ctx, flow, command)
	if err != nil {
		return svc.t("flow.card.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordFlowAudit(ctx, command, "flow.card_published", flow.FlowID, "developer-review flow Mattermost card published")
	return svc.t("flow.card.result", map[string]any{
		"FlowID":  flow.FlowID,
		"PostID":  post.PostID,
		"Channel": post.ChannelID,
	})
}

func (svc *SlashCommandService) handleFlowCleanup(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) != 1 {
		return svc.t("flow.cleanup.usage", nil)
	}
	flowID := strings.ToLower(strings.TrimSpace(args[0]))
	if !validFlowID(flowID) {
		return svc.t("runtime.invalid_run_id", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("flow.storage_not_ready", nil)
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, flowID)
	if err != nil {
		return svc.t("flow.cleanup.failed", map[string]any{"Error": safeError(err)})
	}
	runs, err := svc.cfg.Store.ListAgentRunsByFlowID(ctx, flow.FlowID)
	if err != nil {
		return svc.t("flow.cleanup.failed", map[string]any{"Error": safeError(err)})
	}
	deletedJobs := 0
	deletedPVCs := 0
	for _, run := range runs {
		result, err := svc.cfg.RuntimeRunner.CleanupRun(ctx, run.RunID)
		if err != nil {
			return svc.t("flow.cleanup.failed", map[string]any{"Error": safeError(err)})
		}
		if result.JobDeleted {
			deletedJobs++
		}
		if result.PVCDeleted {
			deletedPVCs++
		}
		_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: run.RunID, Status: "cleaned"})
	}
	flow, _ = svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusCleaned, Summary: "flow runtime resources cleaned"})
	svc.recordFlowAudit(ctx, command, "flow.cleaned", flow.FlowID, "developer-review flow resources cleaned from Mattermost slash command")
	return svc.t("flow.cleanup.result", map[string]any{
		"FlowID":      flow.FlowID,
		"Runs":        len(runs),
		"JobsDeleted": deletedJobs,
		"PVCsDeleted": deletedPVCs,
	})
}

func (svc *SlashCommandService) HandleFlowAction(ctx context.Context, command FlowActionCommand) FlowActionResult {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return FlowActionResult{StatusCode: 503, EphemeralText: svc.t("flow.action.storage_not_ready", nil)}
	}
	flowID := strings.ToLower(strings.TrimSpace(command.FlowID))
	if !validFlowID(flowID) {
		return FlowActionResult{StatusCode: 400, EphemeralText: svc.t("runtime.invalid_run_id", nil)}
	}
	action := strings.ToLower(strings.TrimSpace(command.Action))
	if !validFlowAction(action) {
		return FlowActionResult{StatusCode: 400, EphemeralText: svc.t("flow.action.invalid", nil)}
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, flowID)
	if err != nil {
		return FlowActionResult{StatusCode: 404, EphemeralText: svc.t("flow.action.failed", map[string]any{"Error": safeError(err)})}
	}
	if strings.TrimSpace(flow.ActionToken) == "" || command.Token != flow.ActionToken {
		return FlowActionResult{StatusCode: 401, EphemeralText: svc.t("flow.action.unauthorized", nil)}
	}
	if strings.TrimSpace(flow.OwnerUserID) != "" && strings.TrimSpace(command.UserID) != flow.OwnerUserID {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.owner_only", nil)}
	}

	switch action {
	case flowActionApprove:
		return svc.handleFlowOwnerDecision(ctx, flow, command, flowStatusOwnerApproved, flowOwnerDecisionApproved, "flow.owner_approved", "flow.action.approved")
	case flowActionReject:
		return svc.handleFlowOwnerDecision(ctx, flow, command, flowStatusOwnerRejected, flowOwnerDecisionRejected, "flow.owner_rejected", "flow.action.rejected")
	case flowActionStop:
		return svc.handleFlowOwnerDecision(ctx, flow, command, flowStatusStopped, flowOwnerDecisionStopped, "flow.stopped", "flow.action.stopped")
	case flowActionRerun:
		return svc.handleFlowOwnerRerun(ctx, flow, command)
	default:
		return FlowActionResult{StatusCode: 400, EphemeralText: svc.t("flow.action.invalid", nil)}
	}
}

func (svc *SlashCommandService) handleFlowOwnerDecision(ctx context.Context, flow entity.AgentFlow, command FlowActionCommand, status string, decision string, eventType string, messageID string) FlowActionResult {
	if flowOwnerTerminal(flow.Status) {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.already_final", nil)}
	}
	updated, err := svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{
		FlowID:        flow.FlowID,
		Status:        status,
		OwnerDecision: decision,
		Summary:       "owner decision: " + decision,
	})
	if err != nil {
		return FlowActionResult{StatusCode: 500, EphemeralText: svc.t("flow.action.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordFlowAudit(ctx, SlashCommand{UserID: command.UserID, UserName: command.UserName, ChannelID: command.ChannelID}, eventType, updated.FlowID, "owner decision from Mattermost action: "+decision)
	if err := svc.refreshFlowCard(ctx, updated); err != nil {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.card_failed", map[string]any{"Message": svc.t(messageID, nil), "Error": safeError(err)})}
	}
	return FlowActionResult{StatusCode: 200, EphemeralText: svc.t(messageID, nil)}
}

func (svc *SlashCommandService) handleFlowOwnerRerun(ctx context.Context, flow entity.AgentFlow, command FlowActionCommand) FlowActionResult {
	if flowOwnerTerminal(flow.Status) {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.already_final", nil)}
	}
	if flow.PRNumber <= 0 {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.rerun_no_pr", nil)}
	}
	if !flowCanOwnerRerun(flow) {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.rerun_not_ready", nil)}
	}
	if svc.cfg.RuntimeRunner == nil {
		return FlowActionResult{StatusCode: 503, EphemeralText: svc.t("runtime.not_configured", nil)}
	}
	runID := flowOwnerRerunReviewerRunID(flow.FlowID, time.Now().UTC())
	updated, started, err := svc.startFlowReviewerWithRunID(ctx, flow, runID, "flow reviewer rerun started by owner")
	if err != nil {
		return FlowActionResult{StatusCode: 500, EphemeralText: svc.t("flow.action.failed", map[string]any{"Error": safeError(err)})}
	}
	updated, err = svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{
		FlowID:        updated.FlowID,
		OwnerDecision: flowOwnerDecisionRerun,
		Summary:       "owner requested reviewer rerun",
	})
	if err != nil {
		return FlowActionResult{StatusCode: 500, EphemeralText: svc.t("flow.action.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordFlowAudit(ctx, SlashCommand{UserID: command.UserID, UserName: command.UserName, ChannelID: command.ChannelID}, "flow.rerun_requested", updated.FlowID, "owner requested reviewer rerun from Mattermost action")
	if err := svc.refreshFlowCard(ctx, updated); err != nil {
		return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.card_failed", map[string]any{"Message": svc.t("flow.action.rerun_started", map[string]any{"RunID": started.RunID}), "Error": safeError(err)})}
	}
	return FlowActionResult{StatusCode: 200, EphemeralText: svc.t("flow.action.rerun_started", map[string]any{"RunID": started.RunID})}
}

func (svc *SlashCommandService) publishFlowCard(ctx context.Context, flow entity.AgentFlow, command SlashCommand) (entity.AgentFlow, FlowCardPost, error) {
	if svc.cfg.FlowCardPublisher == nil {
		return flow, FlowCardPost{}, fmt.Errorf("Mattermost flow card publisher is not configured")
	}
	if strings.TrimSpace(svc.cfg.FlowActionURL) == "" {
		return flow, FlowCardPost{}, fmt.Errorf("Mattermost flow action URL is not configured")
	}
	channelID := strings.TrimSpace(flow.ControlChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(command.ChannelID)
	}
	if channelID == "" {
		return flow, FlowCardPost{}, fmt.Errorf("Mattermost channel id is required")
	}
	update := adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID}
	if strings.TrimSpace(flow.ActionToken) == "" {
		update.ActionToken = newFlowActionToken()
	}
	if strings.TrimSpace(flow.OwnerUserID) == "" && strings.TrimSpace(command.UserID) != "" {
		update.OwnerUserID = strings.TrimSpace(command.UserID)
		update.OwnerUser = strings.TrimSpace(command.UserName)
	}
	if strings.TrimSpace(flow.ControlChannelID) == "" {
		update.ControlChannelID = channelID
	}
	if update.ActionToken != "" || update.OwnerUserID != "" || update.ControlChannelID != "" {
		updated, err := svc.cfg.Store.UpdateAgentFlow(ctx, update)
		if err != nil {
			return flow, FlowCardPost{}, err
		}
		flow = updated
	}
	card := svc.flowCard(flow, channelID)
	post, err := svc.cfg.FlowCardPublisher.UpsertFlowCard(ctx, card)
	if err != nil {
		return flow, FlowCardPost{}, err
	}
	if flow.ControlPostID == "" || flow.ControlChannelID == "" {
		updated, err := svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{
			FlowID:           flow.FlowID,
			ControlChannelID: post.ChannelID,
			ControlPostID:    post.PostID,
		})
		if err != nil {
			return flow, post, err
		}
		flow = updated
	}
	return flow, post, nil
}

func (svc *SlashCommandService) refreshFlowCard(ctx context.Context, flow entity.AgentFlow) error {
	if svc.cfg.FlowCardPublisher == nil || svc.cfg.FlowActionURL == "" || flow.ControlChannelID == "" || flow.ControlPostID == "" {
		return nil
	}
	_, err := svc.cfg.FlowCardPublisher.UpsertFlowCard(ctx, svc.flowCard(flow, flow.ControlChannelID))
	return err
}

func (svc *SlashCommandService) flowCard(flow entity.AgentFlow, channelID string) FlowCard {
	return FlowCard{
		ChannelID: channelID,
		PostID:    flow.ControlPostID,
		ActionURL: svc.cfg.FlowActionURL,
		Message:   svc.t("flow.card.message", map[string]any{"FlowID": flow.FlowID}),
		Color:     flowCardColor(flow.Status),
		Title:     svc.t("flow.card.title", map[string]any{"FlowID": flow.FlowID, "Repository": flow.FullName()}),
		Text:      svc.flowCardText(flow),
		Fields:    svc.flowCardFields(flow),
		Actions:   svc.flowCardActions(flow),
	}
}

func (svc *SlashCommandService) flowCardText(flow entity.AgentFlow) string {
	lines := []string{
		svc.t("flow.card.text.status", map[string]any{"Status": flow.Status, "Summary": svc.flowSummaryText(flow)}),
		svc.t("flow.card.text.branch", map[string]any{"Branch": flow.HeadBranch}),
		svc.t("flow.card.text.attempt", map[string]any{"Attempt": flow.Attempt, "MaxAttempts": flow.MaxAttempts}),
	}
	if flow.PRNumber > 0 || strings.TrimSpace(flow.PRURL) != "" {
		lines = append(lines, svc.t("flow.card.text.pr", map[string]any{"PR": flow.PRNumber, "URL": flow.PRURL}))
	}
	if flow.OwnerDecision != "" {
		lines = append(lines, svc.t("flow.card.text.owner_decision", map[string]any{"Decision": flow.OwnerDecision}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) flowCardFields(flow entity.AgentFlow) []FlowCardField {
	fields := []FlowCardField{
		{Title: svc.t("flow.card.field.developer", nil), Value: emptyAsUnknown(flow.CurrentDeveloperRunID), Short: true},
		{Title: svc.t("flow.card.field.reviewer", nil), Value: emptyAsUnknown(flow.CurrentReviewerRunID), Short: true},
	}
	owner := emptyAsUnknown(flow.OwnerUser)
	if flow.OwnerUserID != "" {
		owner = flow.OwnerUserID
		if flow.OwnerUser != "" {
			owner = flow.OwnerUser + " (" + flow.OwnerUserID + ")"
		}
	}
	fields = append(fields, FlowCardField{Title: svc.t("flow.card.field.owner", nil), Value: owner, Short: false})
	return fields
}

func (svc *SlashCommandService) flowCardActions(flow entity.AgentFlow) []FlowCardAction {
	token := flow.ActionToken
	return []FlowCardAction{
		svc.flowCardAction(flow, flowActionApprove, "flow.card.action.approve", "flow.card.action.approve.tooltip", "success", token, !flowCanOwnerDecide(flow)),
		svc.flowCardAction(flow, flowActionReject, "flow.card.action.reject", "flow.card.action.reject.tooltip", "danger", token, !flowCanOwnerDecide(flow)),
		svc.flowCardAction(flow, flowActionRerun, "flow.card.action.rerun", "flow.card.action.rerun.tooltip", "primary", token, !flowCanOwnerRerun(flow)),
		svc.flowCardAction(flow, flowActionStop, "flow.card.action.stop", "flow.card.action.stop.tooltip", "warning", token, flowOwnerTerminal(flow.Status)),
	}
}

func (svc *SlashCommandService) flowCardAction(flow entity.AgentFlow, action string, nameID string, tooltipID string, style string, token string, disabled bool) FlowCardAction {
	return FlowCardAction{
		ID:       action,
		Name:     svc.t(nameID, nil),
		Tooltip:  svc.t(tooltipID, nil),
		Style:    style,
		Disabled: disabled,
		Context: map[string]any{
			"flow_id": flow.FlowID,
			"action":  action,
			"token":   token,
		},
	}
}

type flowRuntimeAccount struct {
	Profile       entity.AgentProfile
	OpenAIAccount entity.OpenAIAccount
	GitHubAccount entity.GitHubAccount
}

func (svc *SlashCommandService) flowAgentRuntime(ctx context.Context, profileName string) (flowRuntimeAccount, string, bool) {
	profile, ok := svc.agentProfile(ctx, profileName)
	if !ok || !profile.Enabled {
		return flowRuntimeAccount{}, svc.t("flow.profile_not_ready", map[string]any{"Profile": profileName}), false
	}
	accountName := defaultString(profile.OpenAIAccountName, "primary")
	account, ok := svc.openAIAccount(ctx, accountName)
	if !ok || account.Status != "authorized" || strings.TrimSpace(account.SecretRef) == "" {
		return flowRuntimeAccount{}, svc.t("flow.openai_account_not_ready", map[string]any{"Account": accountName}), false
	}
	githubAccountName := defaultString(profile.GitHubAccountName, "primary")
	githubAccount, ok := svc.githubAccount(ctx, githubAccountName)
	if !ok || strings.TrimSpace(githubAccount.SecretRef) == "" {
		return flowRuntimeAccount{}, svc.t("flow.github_account_not_ready", map[string]any{"Account": githubAccountName}), false
	}
	return flowRuntimeAccount{Profile: profile, OpenAIAccount: account, GitHubAccount: githubAccount}, "", true
}

func (svc *SlashCommandService) startFlowDeveloperAttempt(ctx context.Context, flow entity.AgentFlow, fix bool) (entity.AgentFlow, runtimerepo.StartedRun, error) {
	account, message, ok := svc.flowAgentRuntime(ctx, "developer")
	if !ok {
		return flow, runtimerepo.StartedRun{}, errors.New(message)
	}
	runID := flowDeveloperRunID(flow.FlowID, flow.Attempt)
	templateKey := developerImplementTaskKey
	baseBranch := flow.BaseBranch
	status := flowStatusDeveloperRunning
	summary := "flow developer attempt started"
	if fix {
		templateKey = developerFixReviewKey
		baseBranch = flow.HeadBranch
		status = flowStatusFixRunning
		summary = "flow developer fix attempt started"
	}
	prompt, err := svc.renderStoredPromptTemplate(ctx, "developer", templateKey, promptTemplateData{
		Run: promptTemplateRunData{
			ID:      runID,
			Profile: "developer",
			Role:    "developer",
			Locale:  svc.cfg.Localizer.Locale(),
		},
		Agent: promptTemplateAgentData{
			Profile: "developer",
			Role:    "developer",
		},
		Repository: promptTemplateRepositoryData{
			Provider: flow.Provider,
			Owner:    flow.Owner,
			Name:     flow.Name,
			FullName: flow.FullName(),
		},
		Task: promptTemplateTaskData{
			Title:      flow.Title,
			Body:       flow.Task,
			BaseBranch: baseBranch,
			HeadBranch: flow.HeadBranch,
		},
		PullRequest: promptTemplatePullRequestData{
			Number:     flow.PRNumber,
			URL:        flow.PRURL,
			Title:      flow.Title,
			BaseBranch: flow.BaseBranch,
			HeadBranch: flow.HeadBranch,
		},
		GitHub: promptGitHubData(defaultString(account.Profile.GitHubAccountName, "agent")),
		Locale: svc.promptTemplateLocaleData(),
	})
	if err != nil {
		return flow, runtimerepo.StartedRun{}, err
	}
	started, err := svc.cfg.RuntimeRunner.StartDeveloperRun(ctx, runtimerepo.DeveloperRunInput{
		RunID:               runID,
		Profile:             "developer",
		CodexAuthSecretName: account.OpenAIAccount.SecretRef,
		GitHubSecretName:    account.GitHubAccount.SecretRef,
		Provider:            flow.Provider,
		Owner:               flow.Owner,
		Name:                flow.Name,
		BaseBranch:          baseBranch,
		HeadBranch:          flow.HeadBranch,
		Title:               flow.Title,
		Task:                flow.Task,
		Prompt:              prompt,
	})
	if err != nil {
		return flow, runtimerepo.StartedRun{}, err
	}
	if _, err := svc.cfg.Store.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID:               runID,
		FlowID:              flow.FlowID,
		ProfileName:         "developer",
		Role:                "developer",
		Provider:            flow.Provider,
		Owner:               flow.Owner,
		Name:                flow.Name,
		BaseBranch:          baseBranch,
		HeadBranch:          flow.HeadBranch,
		Status:              "started",
		KubernetesNamespace: started.Namespace,
		JobName:             started.JobName,
		PVCName:             started.PVCName,
		Summary:             summary,
	}); err != nil {
		return flow, started, err
	}
	flow, err = svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{
		FlowID:                flow.FlowID,
		Status:                status,
		CurrentDeveloperRunID: runID,
		Summary:               summary,
	})
	if err != nil {
		return flow, started, err
	}
	return flow, started, nil
}

func (svc *SlashCommandService) startFlowReviewer(ctx context.Context, flow entity.AgentFlow) (entity.AgentFlow, runtimerepo.StartedRun, error) {
	return svc.startFlowReviewerWithRunID(ctx, flow, flowReviewerRunID(flow.FlowID, flow.Attempt), "flow reviewer attempt started")
}

func (svc *SlashCommandService) startFlowReviewerWithRunID(ctx context.Context, flow entity.AgentFlow, runID string, summary string) (entity.AgentFlow, runtimerepo.StartedRun, error) {
	account, message, ok := svc.flowAgentRuntime(ctx, "reviewer")
	if !ok {
		return flow, runtimerepo.StartedRun{}, errors.New(message)
	}
	prompt, err := svc.renderStoredPromptTemplate(ctx, "reviewer", reviewPRTemplateKey, promptTemplateData{
		Run: promptTemplateRunData{
			ID:      runID,
			Profile: "reviewer",
			Role:    "reviewer",
			Locale:  svc.cfg.Localizer.Locale(),
		},
		Agent: promptTemplateAgentData{
			Profile: "reviewer",
			Role:    "reviewer",
		},
		Repository: promptTemplateRepositoryData{
			Provider: flow.Provider,
			Owner:    flow.Owner,
			Name:     flow.Name,
			FullName: flow.FullName(),
		},
		PullRequest: promptTemplatePullRequestData{
			Number:     flow.PRNumber,
			URL:        flow.PRURL,
			Title:      flow.Title,
			BaseBranch: flow.BaseBranch,
			HeadBranch: flow.HeadBranch,
		},
		GitHub: promptGitHubData(defaultString(account.Profile.GitHubAccountName, "primary")),
		Locale: svc.promptTemplateLocaleData(),
	})
	if err != nil {
		return flow, runtimerepo.StartedRun{}, err
	}
	started, err := svc.cfg.RuntimeRunner.StartReviewRun(ctx, runtimerepo.ReviewRunInput{
		RunID:               runID,
		Profile:             "reviewer",
		CodexAuthSecretName: account.OpenAIAccount.SecretRef,
		GitHubSecretName:    account.GitHubAccount.SecretRef,
		Provider:            flow.Provider,
		Owner:               flow.Owner,
		Name:                flow.Name,
		PRNumber:            flow.PRNumber,
		Prompt:              prompt,
	})
	if err != nil {
		return flow, runtimerepo.StartedRun{}, err
	}
	if _, err := svc.cfg.Store.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID:               runID,
		FlowID:              flow.FlowID,
		ProfileName:         "reviewer",
		Role:                "reviewer",
		Provider:            flow.Provider,
		Owner:               flow.Owner,
		Name:                flow.Name,
		BaseBranch:          flow.BaseBranch,
		HeadBranch:          flow.HeadBranch,
		Status:              "started",
		KubernetesNamespace: started.Namespace,
		JobName:             started.JobName,
		PVCName:             started.PVCName,
		Summary:             summary,
	}); err != nil {
		return flow, started, err
	}
	flow, err = svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{
		FlowID:               flow.FlowID,
		Status:               flowStatusReviewRunning,
		CurrentReviewerRunID: runID,
		Summary:              summary,
	})
	if err != nil {
		return flow, started, err
	}
	return flow, started, nil
}

func (svc *SlashCommandService) advanceFlow(ctx context.Context, flow entity.AgentFlow) (entity.AgentFlow, []string) {
	var events []string
	switch flow.Status {
	case flowStatusDeveloperRunning, flowStatusFixRunning:
		return svc.advanceFlowDeveloper(ctx, flow, events)
	case flowStatusReviewRunning:
		return svc.advanceFlowReviewer(ctx, flow, events)
	default:
		return flow, events
	}
}

func (svc *SlashCommandService) advanceFlowDeveloper(ctx context.Context, flow entity.AgentFlow, events []string) (entity.AgentFlow, []string) {
	if flow.CurrentDeveloperRunID == "" {
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: "developer run id is missing"})
		return flow, append(events, svc.flowEvent("flow.event.blocked_developer_run_missing", nil))
	}
	status, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, flow.CurrentDeveloperRunID)
	if err != nil {
		return flow, append(events, svc.flowEvent("flow.event.developer_status_failed", map[string]any{"Error": safeError(err)}))
	}
	derivedStatus := developerRunStatus(status)
	_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: flow.CurrentDeveloperRunID, Status: derivedStatus, PRURL: status.Artifacts["pr-url"]})
	switch derivedStatus {
	case "running", "pending":
		return flow, append(events, svc.flowEvent("flow.event.developer_running", nil))
	case "failed":
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusDeveloperFailed, Summary: "developer run failed"})
		return flow, append(events, svc.flowEvent("flow.event.developer_failed", nil))
	case "completed_no_changes":
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: "developer finished without changes"})
		return flow, append(events, svc.flowEvent("flow.event.developer_no_changes", nil))
	}
	prURL := strings.TrimSpace(status.Artifacts["pr-url"])
	prNumber := pullRequestNumberFromURL(prURL)
	if prURL == "" || prNumber == 0 {
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: "developer finished without pull request artifact"})
		return flow, append(events, svc.flowEvent("flow.event.developer_no_pr", nil))
	}
	flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusPROpened, PRURL: prURL, PRNumber: prNumber, Summary: "developer opened or updated pull request"})
	events = append(events, svc.flowEvent("flow.event.developer_pr_reviewer_starting", nil))
	updated, started, err := svc.startFlowReviewer(ctx, flow)
	if err != nil {
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: safeError(err)})
		return flow, append(events, svc.flowEvent("flow.event.reviewer_start_failed", map[string]any{"Error": safeError(err)}))
	}
	return updated, append(events, svc.flowEvent("flow.event.reviewer_started", map[string]any{"RunID": started.RunID}))
}

func (svc *SlashCommandService) advanceFlowReviewer(ctx context.Context, flow entity.AgentFlow, events []string) (entity.AgentFlow, []string) {
	if flow.CurrentReviewerRunID == "" {
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: "reviewer run id is missing"})
		return flow, append(events, svc.flowEvent("flow.event.blocked_reviewer_run_missing", nil))
	}
	status, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, flow.CurrentReviewerRunID)
	if err != nil {
		return flow, append(events, svc.flowEvent("flow.event.reviewer_status_failed", map[string]any{"Error": safeError(err)}))
	}
	derivedStatus := reviewerRunStatus(status)
	_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: flow.CurrentReviewerRunID, Status: derivedStatus, PRURL: status.Artifacts["pr-url"]})
	switch derivedStatus {
	case "running", "pending":
		return flow, append(events, svc.flowEvent("flow.event.reviewer_running", nil))
	case "failed":
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusReviewerFailed, Summary: "reviewer run failed"})
		return flow, append(events, svc.flowEvent("flow.event.reviewer_failed", nil))
	case "approved":
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusApprovedByReviewer, Summary: "reviewer approved pull request"})
		return flow, append(events, svc.flowEvent("flow.event.reviewer_approved", nil))
	case "changes_requested":
		if flow.Attempt >= flow.MaxAttempts {
			flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: "reviewer requested changes and attempt limit reached"})
			return flow, append(events, svc.flowEvent("flow.event.blocked_attempt_limit", nil))
		}
		nextAttempt := flow.Attempt + 1
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusChangesRequested, Attempt: nextAttempt, Summary: "reviewer requested changes; starting fix attempt"})
		updated, started, err := svc.startFlowDeveloperAttempt(ctx, flow, true)
		if err != nil {
			flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: safeError(err)})
			return flow, append(events, svc.flowEvent("flow.event.fix_start_failed", map[string]any{"Error": safeError(err)}))
		}
		return updated, append(events, svc.flowEvent("flow.event.fix_started", map[string]any{"RunID": started.RunID}))
	case "review_comment", "review_submitted", "succeeded":
		flow = svc.updateFlowBestEffort(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusWaitingOwner, Summary: "reviewer completed with a non-approval decision"})
		return flow, append(events, svc.flowEvent("flow.event.waiting_owner", nil))
	default:
		return flow, append(events, svc.flowEvent("flow.event.reviewer_status", map[string]any{"Status": derivedStatus}))
	}
}

func (svc *SlashCommandService) updateFlowBestEffort(ctx context.Context, input adminrepo.UpdateAgentFlowInput) entity.AgentFlow {
	flow, err := svc.cfg.Store.UpdateAgentFlow(ctx, input)
	if err == nil {
		return flow
	}
	existing, getErr := svc.cfg.Store.GetAgentFlow(ctx, input.FlowID)
	if getErr == nil {
		return existing
	}
	return entity.AgentFlow{FlowID: input.FlowID, Status: input.Status, Summary: safeError(err)}
}

func (svc *SlashCommandService) flowStatusText(flow entity.AgentFlow, events []string) string {
	lines := []string{
		svc.t("flow.status.header", nil),
		svc.t("flow.status.flow", map[string]any{
			"FlowID":      flow.FlowID,
			"Status":      flow.Status,
			"Repository":  flow.FullName(),
			"Branch":      flow.HeadBranch,
			"Attempt":     flow.Attempt,
			"MaxAttempts": flow.MaxAttempts,
		}),
	}
	if flow.PRNumber > 0 || strings.TrimSpace(flow.PRURL) != "" {
		lines = append(lines, svc.t("flow.status.pr", map[string]any{"PR": flow.PRNumber, "URL": flow.PRURL}))
	}
	if flow.CurrentDeveloperRunID != "" {
		lines = append(lines, svc.t("flow.status.developer", map[string]any{"RunID": flow.CurrentDeveloperRunID}))
	}
	if flow.CurrentReviewerRunID != "" {
		lines = append(lines, svc.t("flow.status.reviewer", map[string]any{"RunID": flow.CurrentReviewerRunID}))
	}
	if summary := svc.flowSummaryText(flow); summary != "" {
		lines = append(lines, svc.t("flow.status.summary", map[string]any{"Summary": summary}))
	}
	lines = append(lines, events...)
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) flowEvent(messageID string, data map[string]any) string {
	return svc.t("flow.status.event", map[string]any{"Event": svc.t(messageID, data)})
}

func (svc *SlashCommandService) flowSummaryText(flow entity.AgentFlow) string {
	switch flow.Status {
	case flowStatusApprovedByReviewer:
		return svc.t("flow.summary.approved_by_reviewer", nil)
	case flowStatusBlocked:
		return svc.t("flow.summary.blocked", nil)
	case flowStatusChangesRequested:
		return svc.t("flow.summary.changes_requested", nil)
	case flowStatusCleaned:
		return svc.t("flow.summary.cleaned", nil)
	case flowStatusCreated:
		return svc.t("flow.summary.created", nil)
	case flowStatusDeveloperFailed:
		return svc.t("flow.summary.developer_failed", nil)
	case flowStatusDeveloperRunning:
		return svc.t("flow.summary.developer_running", nil)
	case flowStatusFixRunning:
		return svc.t("flow.summary.fix_running", nil)
	case flowStatusOwnerApproved:
		return svc.t("flow.summary.owner_approved", nil)
	case flowStatusOwnerRejected:
		return svc.t("flow.summary.owner_rejected", nil)
	case flowStatusPROpened:
		return svc.t("flow.summary.pr_opened", nil)
	case flowStatusReviewerFailed:
		return svc.t("flow.summary.reviewer_failed", nil)
	case flowStatusReviewRunning:
		return svc.t("flow.summary.review_running", nil)
	case flowStatusStopped:
		return svc.t("flow.summary.stopped", nil)
	case flowStatusWaitingOwner:
		return svc.t("flow.summary.waiting_owner", nil)
	default:
		return sanitizeLogTail(flow.Summary)
	}
}

func (svc *SlashCommandService) handleLocale(args []string) string {
	if len(args) == 1 && args[0] == "get" {
		return svc.t("locale.get", map[string]any{
			"Locale":    svc.cfg.Localizer.Locale(),
			"Supported": supportedLocalesText(svc.cfg.Localizer),
		})
	}
	if len(args) == 2 && args[0] == "set" {
		locale, err := svc.cfg.Localizer.SetLocale(args[1])
		if err != nil {
			return svc.t("locale.unsupported", map[string]any{
				"Locale":    args[1],
				"Supported": supportedLocalesText(svc.cfg.Localizer),
			})
		}
		return svc.t("locale.set", map[string]any{"Locale": locale})
	}
	return svc.t("locale.usage", nil)
}

func (svc *SlashCommandService) handleProfile(ctx context.Context, args []string) string {
	if len(args) != 1 || args[0] != "list" {
		return svc.t("profile.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("profile.list.storage_not_ready", nil)
	}
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		return svc.t("profile.list.read_failed", map[string]any{"Error": safeError(err)})
	}
	if len(profiles) == 0 {
		return svc.t("profile.list.empty", nil)
	}
	lines := []string{svc.t("profile.list.header", nil)}
	for _, profile := range profiles {
		enabled := svc.t("label.disabled", nil)
		if profile.Enabled {
			enabled = svc.t("label.enabled", nil)
		}
		lines = append(lines, svc.t("profile.list.item", map[string]any{
			"Name":        profile.Name,
			"Role":        profile.Role,
			"Enabled":     enabled,
			"Account":     defaultString(profile.OpenAIAccountName, "primary"),
			"GitHub":      defaultString(profile.GitHubAccountName, "primary"),
			"Description": profile.Description,
		}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handlePrompt(ctx context.Context, raw string, command SlashCommand) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("prompt.storage_not_ready", nil)
	}
	action, rest := consumeToken(raw)
	switch action {
	case "":
		return svc.t("prompt.usage", nil)
	case "help":
		return svc.handlePromptHelp(rest)
	case "list":
		return svc.handlePromptList(ctx, rest)
	case "show":
		return svc.handlePromptShow(ctx, rest)
	case "render":
		return svc.handlePromptRender(ctx, rest)
	case "set":
		return svc.handlePromptSet(ctx, rest, command)
	default:
		return svc.t("prompt.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handlePromptHelp(raw string) string {
	profileName, rest := consumeToken(raw)
	templateKey, _ := consumeToken(rest)
	if profileName == "" {
		profileName = "reviewer"
	}
	if templateKey == "" {
		templateKey = reviewPRTemplateKey
	}
	return svc.t("prompt.help", map[string]any{
		"Profile":     profileName,
		"TemplateKey": templateKey,
		"Reference":   svc.t("prompt.help.reference", promptTemplateReferenceData()),
	})
}

func (svc *SlashCommandService) handlePromptList(ctx context.Context, raw string) string {
	profileName, rest := consumeToken(raw)
	if strings.TrimSpace(rest) != "" {
		return svc.t("prompt.list.usage", nil)
	}
	templates, err := svc.cfg.Store.ListAgentPromptTemplates(ctx, profileName)
	if err != nil {
		return svc.t("prompt.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(templates) == 0 {
		return svc.t("prompt.list.empty", nil)
	}
	lines := []string{svc.t("prompt.list.header", nil)}
	for _, item := range templates {
		lines = append(lines, svc.t("prompt.list.item", map[string]any{
			"Profile":     item.ProfileName,
			"TemplateKey": item.TemplateKey,
			"Bytes":       len(item.Body),
		}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handlePromptShow(ctx context.Context, raw string) string {
	profileName, templateKey, body, ok := parsePromptTemplateCommand(raw)
	if !ok || body != "" {
		return svc.t("prompt.show.usage", nil)
	}
	if !validPromptTemplateID(profileName) || !validPromptTemplateID(templateKey) {
		return svc.t("prompt.invalid_id", nil)
	}
	item, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, profileName, templateKey)
	if err != nil {
		return svc.t("prompt.show.failed", map[string]any{"Error": safeError(err)})
	}
	return svc.t("prompt.show.result", map[string]any{
		"Profile":     item.ProfileName,
		"TemplateKey": item.TemplateKey,
		"Body":        sanitizeLogTail(item.Body),
	})
}

func (svc *SlashCommandService) handlePromptRender(ctx context.Context, raw string) string {
	profileName, templateKey, body, ok := parsePromptTemplateCommand(raw)
	if !ok {
		return svc.t("prompt.render.usage", nil)
	}
	if !validPromptTemplateID(profileName) || !validPromptTemplateID(templateKey) {
		return svc.t("prompt.invalid_id", nil)
	}
	if body == "" {
		item, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, profileName, templateKey)
		if err != nil {
			return svc.t("prompt.render.failed", map[string]any{"Error": safeError(err)})
		}
		body = item.Body
	}
	rendered, err := renderAgentPromptTemplate(body, samplePromptTemplateData(profileName, templateKey, svc.promptTemplateLocaleData()))
	if err != nil {
		return svc.t("prompt.render.failed", map[string]any{"Error": safeError(err)})
	}
	return svc.t("prompt.render.result", map[string]any{
		"Profile":     profileName,
		"TemplateKey": templateKey,
		"Rendered":    sanitizeLogTail(rendered),
	})
}

func (svc *SlashCommandService) handlePromptSet(ctx context.Context, raw string, command SlashCommand) string {
	profileName, templateKey, body, ok := parsePromptTemplateCommand(raw)
	if !ok || strings.TrimSpace(body) == "" {
		return svc.t("prompt.set.usage", nil)
	}
	if !validPromptTemplateID(profileName) || !validPromptTemplateID(templateKey) {
		return svc.t("prompt.invalid_id", nil)
	}
	rendered, err := renderAgentPromptTemplate(body, samplePromptTemplateData(profileName, templateKey, svc.promptTemplateLocaleData()))
	if err != nil {
		return svc.t("prompt.set.render_failed", map[string]any{"Error": safeError(err)})
	}
	item, created, err := svc.cfg.Store.UpsertAgentPromptTemplate(ctx, adminrepo.UpsertAgentPromptTemplateInput{
		ProfileName: profileName,
		TemplateKey: templateKey,
		Body:        body,
	})
	if err != nil {
		return svc.t("prompt.set.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordPromptAudit(ctx, command, "agent_prompt_template.upserted", item.ProfileName+"/"+item.TemplateKey, "agent prompt template upserted from Mattermost slash command")
	state := svc.t("label.updated", nil)
	if created {
		state = svc.t("label.created", nil)
	}
	return svc.t("prompt.set.result", map[string]any{
		"State":       state,
		"Profile":     item.ProfileName,
		"TemplateKey": item.TemplateKey,
		"Rendered":    sanitizeLogTail(rendered),
	})
}

func (svc *SlashCommandService) handleOpenAI(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return svc.t("openai.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("openai.storage_not_ready", nil)
	}
	switch args[0] {
	case "auth":
		return svc.handleOpenAIAuth(ctx, args[1:], command)
	case "status":
		return svc.handleOpenAIStatus(ctx, args[1:], command)
	case "list":
		return svc.handleOpenAIList(ctx)
	case "cleanup":
		return svc.handleOpenAICleanup(ctx, args[1:], command)
	default:
		return svc.t("openai.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleOpenAIAuth(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) != 1 {
		return svc.t("openai.auth.usage", nil)
	}
	accountName, err := parseOpenAIAccountName(args[0])
	if err != nil {
		return svc.validationErrorText(err)
	}
	secretName := codexAuthSecretName(svc.cfg.CodexAuthSecretName, accountName)
	account, created, err := svc.cfg.Store.UpsertOpenAIAccount(ctx, adminrepo.UpsertOpenAIAccountInput{
		Name:           accountName,
		CredentialName: openAICredentialName(accountName),
		SecretRef:      secretName,
		Status:         "auth_pending",
	})
	if err != nil {
		return svc.t("openai.auth.store_failed", map[string]any{"Error": safeError(err)})
	}
	session, err := svc.cfg.RuntimeRunner.StartCodexAuthSession(ctx, runtimerepo.CodexAuthSessionInput{AccountName: account.Name, SecretName: account.SecretRef})
	if err != nil {
		_, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{Name: accountName, SecretRef: secretName, Status: "auth_failed"})
		return svc.t("openai.auth.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordOpenAIAudit(ctx, command, "openai.account.auth_started", account.Name, "openai account device-code auth session started from Mattermost slash command")
	state := svc.t("label.updated", nil)
	if created {
		state = svc.t("label.created", nil)
	}
	return svc.t("openai.auth.started", map[string]any{
		"State":     state,
		"Account":   account.Name,
		"Secret":    account.SecretRef,
		"Namespace": session.Namespace,
		"Job":       session.JobName,
		"Created":   session.Created,
	})
}

func (svc *SlashCommandService) handleOpenAIStatus(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) != 1 {
		return svc.t("openai.status.usage", nil)
	}
	accountName, err := parseOpenAIAccountName(args[0])
	if err != nil {
		return svc.validationErrorText(err)
	}
	account, ok := svc.openAIAccount(ctx, accountName)
	if !ok {
		return svc.t("openai.status.account_not_found", map[string]any{"Account": accountName})
	}
	status, err := svc.cfg.RuntimeRunner.GetCodexAuthStatus(ctx, account.Name, account.SecretRef)
	if err != nil {
		return svc.t("openai.status.failed", map[string]any{"Error": safeError(err)})
	}
	if !status.Exists {
		return svc.t("openai.status.session_not_found", map[string]any{"Account": account.Name, "Status": account.Status})
	}
	if status.AuthReady {
		completed, err := svc.cfg.RuntimeRunner.CompleteCodexAuthSession(ctx, runtimerepo.CodexAuthCompleteInput{AccountName: account.Name, SecretName: account.SecretRef})
		if err != nil {
			return svc.t("openai.status.complete_failed", map[string]any{"Error": safeError(err)})
		}
		account, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{Name: account.Name, SecretRef: completed.SecretName, Status: "authorized"})
		_, _ = svc.cfg.RuntimeRunner.CleanupCodexAuthSession(ctx, account.Name)
		svc.recordOpenAIAudit(ctx, command, "openai.account.authorized", account.Name, "openai account auth.json saved to Kubernetes Secret")
		return svc.t("openai.status.authorized", map[string]any{
			"Account": account.Name,
			"Secret":  completed.SecretName,
			"Saved":   completed.Saved,
		})
	}
	if status.DeviceURL != "" && status.DeviceCode != "" {
		_, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{Name: account.Name, SecretRef: account.SecretRef, Status: "awaiting_user"})
		return svc.t("openai.status.device_code", map[string]any{
			"Account": account.Name,
			"URL":     status.DeviceURL,
			"Code":    status.DeviceCode,
			"Job":     status.JobName,
			"Pod":     emptyAsUnknown(status.PodName),
			"Phase":   emptyAsUnknown(status.PodPhase),
		})
	}
	if status.JobFailed > 0 {
		_, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{Name: account.Name, SecretRef: account.SecretRef, Status: "auth_failed"})
		return svc.t("openai.status.job_failed", map[string]any{"Account": account.Name, "Job": status.JobName})
	}
	return svc.t("openai.status.waiting", map[string]any{
		"Account": account.Name,
		"Job":     status.JobName,
		"Pod":     emptyAsUnknown(status.PodName),
		"Phase":   emptyAsUnknown(status.PodPhase),
	})
}

func (svc *SlashCommandService) handleOpenAIList(ctx context.Context) string {
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		return svc.t("openai.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(accounts) == 0 {
		return svc.t("openai.list.empty", nil)
	}
	lines := []string{svc.t("openai.list.header", nil)}
	for _, account := range accounts {
		lines = append(lines, svc.t("openai.list.item", map[string]any{
			"Account": account.Name,
			"Status":  account.Status,
			"Secret":  account.SecretRef,
		}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleOpenAICleanup(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) != 1 {
		return svc.t("openai.cleanup.usage", nil)
	}
	accountName, err := parseOpenAIAccountName(args[0])
	if err != nil {
		return svc.validationErrorText(err)
	}
	result, err := svc.cfg.RuntimeRunner.CleanupCodexAuthSession(ctx, accountName)
	if err != nil {
		return svc.t("openai.cleanup.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordOpenAIAudit(ctx, command, "openai.account.auth_cleanup", accountName, "openai account auth job cleaned from Mattermost slash command")
	return svc.t("openai.cleanup.result", map[string]any{
		"Account":    result.AccountName,
		"Namespace":  result.Namespace,
		"JobDeleted": result.JobDeleted,
	})
}

func (svc *SlashCommandService) helpText() string {
	commands := []string{
		svc.t("help.status", nil),
		svc.t("help.repo_add", nil),
		svc.t("help.repo_list", nil),
		svc.t("help.github_check", nil),
		svc.t("help.github_branch_dry_run", nil),
		svc.t("help.github_branch_create", nil),
		svc.t("help.github_pr_dry_run", nil),
		svc.t("help.github_pr_status", nil),
		svc.t("help.github_webhook", nil),
		svc.t("help.token_check", nil),
		svc.t("help.openai", nil),
		svc.t("help.profile_list", nil),
		svc.t("help.prompt", nil),
		svc.t("help.locale", nil),
		svc.t("help.runtime", nil),
		svc.t("help.dev", nil),
		svc.t("help.review", nil),
		svc.t("help.flow", nil),
	}
	return svc.t("help.header", nil) + "\n" + strings.Join(commands, "\n")
}

func (svc *SlashCommandService) handleGitHub(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RepositoryProvider == nil {
		return svc.t("github.provider_not_configured", nil)
	}
	if len(args) == 0 {
		return svc.t("github.usage", nil)
	}
	switch args[0] {
	case "check":
		return svc.handleGitHubCheck(ctx, args[1:], command)
	case "branch":
		return svc.handleGitHubBranch(ctx, args[1:], command)
	case "pr":
		return svc.handleGitHubPR(ctx, args[1:], command)
	case "webhook":
		return svc.handleGitHubWebhook(ctx, args[1:], command)
	default:
		return svc.t("github.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleGitHubCheck(ctx context.Context, args []string, command SlashCommand) string {
	ref, err := parseRepositoryRef(args, "github check")
	if err != nil {
		return svc.validationErrorText(err)
	}
	access, err := svc.cfg.RepositoryProvider.CheckRepository(ctx, ref.Owner, ref.Name)
	if err != nil {
		return svc.t("github.check.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordGitHubAudit(ctx, command, "github.repository.checked", access.Owner+"/"+access.Name, "github repository access checked")
	return svc.t("github.check.result", map[string]any{
		"Provider":      access.Provider,
		"Owner":         access.Owner,
		"Name":          access.Name,
		"DefaultBranch": access.DefaultBranch,
		"Private":       access.Private,
		"CanPull":       access.CanPull,
		"CanPush":       access.CanPush,
		"CanMaintain":   access.CanMaintain,
		"CanAdmin":      access.CanAdmin,
	})
}

func (svc *SlashCommandService) handleGitHubBranch(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) < 3 {
		return svc.t("github.branch.usage", nil)
	}
	mode := args[0]
	if mode != "dry-run" && mode != "create" {
		return svc.t("github.branch.unsupported_mode", nil)
	}
	ref, err := parseRepositoryRef(args[1:2], "github branch")
	if err != nil {
		return svc.validationErrorText(err)
	}
	branch := args[2]
	base := "main"
	if len(args) >= 4 {
		base = args[3]
	}
	if !validBranch(branch) || !validBranch(base) {
		return svc.t("github.branch.invalid_branch", nil)
	}
	if mode == "dry-run" {
		baseRef, err := svc.cfg.RepositoryProvider.ResolveBranch(ctx, ref.Owner, ref.Name, base)
		if err != nil {
			return svc.t("github.branch.dry_run_failed", map[string]any{"Error": safeError(err)})
		}
		return svc.t("github.branch.dry_run_result", map[string]any{
			"Owner":   ref.Owner,
			"Name":    ref.Name,
			"Branch":  branch,
			"Base":    base,
			"BaseSHA": shortSHA(baseRef.SHA),
		})
	}
	created, err := svc.cfg.RepositoryProvider.CreateBranch(ctx, ref.Owner, ref.Name, branch, base)
	if err != nil {
		return svc.t("github.branch.create_failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordGitHubAudit(ctx, command, "github.branch.created", ref.Owner+"/"+ref.Name+":"+branch, "github branch created from Mattermost slash command")
	return svc.t("github.branch.create_result", map[string]any{
		"Owner":  created.Owner,
		"Name":   created.Name,
		"Branch": created.Branch,
		"SHA":    shortSHA(created.SHA),
	})
}

func (svc *SlashCommandService) handleGitHubPR(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return svc.t("github.pr.usage", nil)
	}
	switch args[0] {
	case "dry-run":
		return svc.handleGitHubPRDryRun(ctx, args[1:])
	case "create":
		return svc.handleGitHubPRCreate(ctx, args[1:], command)
	case "status":
		return svc.handleGitHubPRStatus(ctx, args[1:])
	default:
		return svc.t("github.pr.unsupported_mode", nil)
	}
}

func (svc *SlashCommandService) handleGitHubWebhook(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) != 2 || args[0] != "ensure" {
		return svc.t("github.webhook.usage", nil)
	}
	ref, err := parseRepositoryRef(args[1:], "github webhook ensure")
	if err != nil {
		return svc.validationErrorText(err)
	}
	lines := svc.ensureRepositoryWebhookLine(ctx, command, "github", ref.Owner, ref.Name)
	if len(lines) == 0 {
		return svc.t("github.webhook.not_registered", nil)
	}
	return svc.t("github.webhook.header", nil) + "\n" + strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleGitHubPRDryRun(ctx context.Context, args []string) string {
	input, err := parsePullRequestInput(args)
	if err != nil {
		return svc.validationErrorText(err)
	}
	preview, err := svc.cfg.RepositoryProvider.PreviewPullRequest(ctx, input)
	if err != nil {
		return svc.t("github.pr.dry_run_failed", map[string]any{"Error": safeError(err)})
	}
	return svc.t("github.pr.dry_run_result", map[string]any{
		"Owner":   preview.Owner,
		"Name":    preview.Name,
		"Head":    preview.Head,
		"HeadSHA": shortSHA(preview.HeadSHA),
		"Base":    preview.Base,
		"BaseSHA": shortSHA(preview.BaseSHA),
		"Title":   preview.Title,
	})
}

func (svc *SlashCommandService) ensureRepositoryWebhookLine(ctx context.Context, command SlashCommand, provider string, owner string, name string) []string {
	if provider != "github" {
		return nil
	}
	if svc.cfg.RepositoryProvider == nil {
		return []string{svc.t("webhook.skipped_provider", nil)}
	}
	if !svc.cfg.GitHubWebhookConfigured {
		return []string{svc.t("webhook.skipped_secret", nil)}
	}
	registration, err := svc.cfg.RepositoryProvider.EnsureRepositoryWebhook(ctx, owner, name)
	if err != nil {
		return []string{svc.t("webhook.not_registered_error", map[string]any{"Error": safeError(err)})}
	}
	svc.recordGitHubAudit(ctx, command, "github.webhook.ensured", owner+"/"+name, "github repository webhook ensured")
	action := svc.t("label.updated", nil)
	if registration.Created {
		action = svc.t("label.created", nil)
	}
	return []string{
		svc.t("webhook.result", map[string]any{"Action": action, "ID": registration.ID, "Active": registration.Active}),
		svc.t("webhook.events", map[string]any{"Events": strings.Join(registration.Events, "`, `")}),
	}
}

func (svc *SlashCommandService) handleGitHubPRCreate(ctx context.Context, args []string, command SlashCommand) string {
	input, err := parsePullRequestInput(args)
	if err != nil {
		return svc.validationErrorText(err)
	}
	input.Draft = true
	input.Body = svc.t("github.pr.create.body", nil)
	summary, err := svc.cfg.RepositoryProvider.CreatePullRequest(ctx, input)
	if err != nil {
		return svc.t("github.pr.create_failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordGitHubAudit(ctx, command, "github.pull_request.created", input.Owner+"/"+input.Name+"#"+strconv.Itoa(summary.Number), "github draft pull request created from Mattermost slash command")
	return svc.t("github.pr.create_result", map[string]any{
		"Owner":  summary.Owner,
		"Name":   summary.Name,
		"Number": summary.Number,
		"Title":  summary.Title,
		"State":  summary.State,
		"URL":    summary.URL,
	})
}

func (svc *SlashCommandService) handleGitHubPRStatus(ctx context.Context, args []string) string {
	if len(args) != 2 {
		return svc.t("github.pr.status.usage", nil)
	}
	ref, err := parseRepositoryRef(args[:1], "github pr status")
	if err != nil {
		return svc.validationErrorText(err)
	}
	number, err := strconv.Atoi(args[1])
	if err != nil || number <= 0 {
		return svc.t("github.pr.status.invalid_number", nil)
	}
	summary, err := svc.cfg.RepositoryProvider.GetPullRequest(ctx, ref.Owner, ref.Name, number)
	if err != nil {
		return svc.t("github.pr.status.failed", map[string]any{"Error": safeError(err)})
	}
	lines := []string{
		svc.t("github.pr.status.header", nil),
		svc.t("github.pr.status.repo", map[string]any{"Owner": summary.Owner, "Name": summary.Name}),
		svc.t("github.pr.status.pr", map[string]any{"Number": summary.Number, "Title": summary.Title}),
		svc.t("github.pr.status.state", map[string]any{
			"State":     summary.State,
			"Draft":     summary.Draft,
			"Merged":    summary.Merged,
			"Mergeable": emptyAsUnknown(summary.MergeableState),
		}),
		svc.t("github.pr.status.counts", map[string]any{
			"Reviews":        summary.ReviewCount,
			"ReviewComments": summary.ReviewCommentCount,
		}),
	}
	for _, review := range summary.LatestReviews {
		lines = append(lines, svc.t("github.pr.status.review", map[string]any{
			"State":  review.State,
			"Author": emptyAsUnknown(review.Author),
		}))
	}
	if summary.URL != "" {
		lines = append(lines, svc.t("github.pr.status.url", map[string]any{"URL": summary.URL}))
	}
	return strings.Join(lines, "\n")
}

type commandValidationError struct {
	messageID string
	data      map[string]any
}

func (err commandValidationError) Error() string {
	return err.messageID
}

func validationError(messageID string, data map[string]any) error {
	return commandValidationError{messageID: messageID, data: data}
}

func (svc *SlashCommandService) validationErrorText(err error) string {
	var commandErr commandValidationError
	if errors.As(err, &commandErr) {
		return svc.t("slash.error", map[string]any{"Message": svc.t(commandErr.messageID, commandErr.data)})
	}
	return svc.t("slash.error", map[string]any{"Message": safeError(err)})
}

func parseRepoAdd(args []string) (adminrepo.UpsertRepositoryInput, error) {
	if len(args) == 0 {
		return adminrepo.UpsertRepositoryInput{}, validationError("parse.repo_add.required", nil)
	}
	provider := "github"
	repoArg := args[0]
	branch := "main"
	if isProvider(repoArg) {
		provider = repoArg
		if len(args) < 2 {
			return adminrepo.UpsertRepositoryInput{}, validationError("parse.repo_add.owner_name_after_provider", nil)
		}
		repoArg = args[1]
		if len(args) >= 3 {
			branch = args[2]
		}
	} else if len(args) >= 2 {
		branch = args[1]
	}
	owner, name, ok := strings.Cut(repoArg, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" {
		return adminrepo.UpsertRepositoryInput{}, validationError("parse.repository.format", nil)
	}
	if !validIdentifier(owner) || !validIdentifier(name) || !validBranch(branch) {
		return adminrepo.UpsertRepositoryInput{}, validationError("parse.repository.invalid_repository_or_branch", nil)
	}
	return adminrepo.UpsertRepositoryInput{
		Provider:      provider,
		Owner:         strings.ToLower(owner),
		Name:          strings.ToLower(name),
		DefaultBranch: branch,
	}, nil
}

type repositoryRef struct {
	Owner string
	Name  string
}

func parseRepositoryRef(args []string, command string) (repositoryRef, error) {
	if len(args) != 1 {
		return repositoryRef{}, validationError("parse.repository.required", map[string]any{"Command": command})
	}
	owner, name, ok := strings.Cut(args[0], "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" {
		return repositoryRef{}, validationError("parse.repository.format", nil)
	}
	if !validIdentifier(owner) || !validIdentifier(name) {
		return repositoryRef{}, validationError("parse.repository.invalid", nil)
	}
	return repositoryRef{
		Owner: strings.ToLower(owner),
		Name:  strings.ToLower(name),
	}, nil
}

func parsePullRequestInput(args []string) (providerrepo.PullRequestInput, error) {
	if len(args) < 4 {
		return providerrepo.PullRequestInput{}, validationError("parse.pr.usage", nil)
	}
	ref, err := parseRepositoryRef(args[:1], "github pr")
	if err != nil {
		return providerrepo.PullRequestInput{}, err
	}
	head := args[1]
	base := args[2]
	title := strings.TrimSpace(strings.Join(args[3:], " "))
	if !validBranch(head) || !validBranch(base) {
		return providerrepo.PullRequestInput{}, validationError("parse.branch.invalid", nil)
	}
	if title == "" {
		return providerrepo.PullRequestInput{}, validationError("parse.pr.title_empty", nil)
	}
	return providerrepo.PullRequestInput{
		Owner: ref.Owner,
		Name:  ref.Name,
		Head:  head,
		Base:  base,
		Title: title,
	}, nil
}

func parseOpenAIAccountName(value string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	if !validRuntimeRunID(name) {
		return "", validationError("parse.openai_account.invalid", nil)
	}
	return name, nil
}

func parsePromptTemplateCommand(raw string) (string, string, string, bool) {
	profileName, rest := consumeToken(raw)
	templateKey, body := consumeToken(rest)
	if profileName == "" || templateKey == "" {
		return "", "", "", false
	}
	return strings.ToLower(profileName), strings.ToLower(templateKey), strings.TrimSpace(body), true
}

func isProvider(value string) bool {
	switch value {
	case "github", "gitlab":
		return true
	default:
		return false
	}
}

var (
	identifierRE       = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	branchRE           = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
	promptTemplateIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	runtimeRunRE       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,47}[a-z0-9])?$`)
	githubPRURLRE      = regexp.MustCompile(`/pull/([0-9]+)(?:$|[/?#])`)
)

const (
	defaultFlowMaxAttempts = 3

	flowStatusApprovedByReviewer = "approved_by_reviewer"
	flowStatusBlocked            = "blocked"
	flowStatusChangesRequested   = "changes_requested"
	flowStatusCleaned            = "cleaned"
	flowStatusCreated            = "created"
	flowStatusDeveloperFailed    = "developer_failed"
	flowStatusDeveloperRunning   = "developer_running"
	flowStatusFixRunning         = "fix_running"
	flowStatusOwnerApproved      = "owner_approved"
	flowStatusOwnerRejected      = "owner_rejected"
	flowStatusPROpened           = "pr_opened"
	flowStatusReviewerFailed     = "reviewer_failed"
	flowStatusReviewRunning      = "review_running"
	flowStatusStopped            = "stopped"
	flowStatusWaitingOwner       = "waiting_owner"

	flowActionApprove = "approve"
	flowActionReject  = "reject"
	flowActionRerun   = "rerun"
	flowActionStop    = "stop"

	flowOwnerDecisionApproved = "approved"
	flowOwnerDecisionRejected = "rejected"
	flowOwnerDecisionRerun    = "rerun"
	flowOwnerDecisionStopped  = "stopped"
)

func validIdentifier(value string) bool {
	return identifierRE.MatchString(value)
}

func validBranch(value string) bool {
	return branchRE.MatchString(value)
}

func validRuntimeRunID(value string) bool {
	return runtimeRunRE.MatchString(value)
}

func validFlowID(value string) bool {
	return validRuntimeRunID(value) && len(value) <= 44
}

func validPromptTemplateID(value string) bool {
	return promptTemplateIDRE.MatchString(value)
}

func flowHeadBranch(flowID string) string {
	return "matter-codex-flow-" + flowID
}

func flowDeveloperRunID(flowID string, attempt int) string {
	return fmt.Sprintf("%s-d%d", flowID, attempt)
}

func flowReviewerRunID(flowID string, attempt int) string {
	return fmt.Sprintf("%s-r%d", flowID, attempt)
}

func flowOwnerRerunReviewerRunID(flowID string, now time.Time) string {
	suffix := "-rr-" + strconv.FormatInt(now.Unix(), 36)
	maxPrefix := 48 - len(suffix)
	prefix := flowID
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-")
	}
	if prefix == "" {
		prefix = "flow"
	}
	return prefix + suffix
}

func validFlowAction(value string) bool {
	switch value {
	case flowActionApprove, flowActionReject, flowActionRerun, flowActionStop:
		return true
	default:
		return false
	}
}

func flowOwnerTerminal(status string) bool {
	switch status {
	case flowStatusOwnerApproved, flowStatusOwnerRejected, flowStatusStopped, flowStatusCleaned:
		return true
	default:
		return false
	}
}

func flowCanOwnerDecide(flow entity.AgentFlow) bool {
	if flowOwnerTerminal(flow.Status) {
		return false
	}
	switch flow.Status {
	case flowStatusApprovedByReviewer, flowStatusWaitingOwner, flowStatusBlocked:
		return flow.PRNumber > 0 || strings.TrimSpace(flow.PRURL) != ""
	default:
		return false
	}
}

func flowCanOwnerRerun(flow entity.AgentFlow) bool {
	if flowOwnerTerminal(flow.Status) || flow.PRNumber <= 0 {
		return false
	}
	switch flow.Status {
	case flowStatusApprovedByReviewer, flowStatusWaitingOwner, flowStatusBlocked, flowStatusReviewerFailed:
		return true
	default:
		return false
	}
}

func flowCardColor(status string) string {
	switch status {
	case flowStatusOwnerApproved, flowStatusApprovedByReviewer:
		return "#2f9e44"
	case flowStatusOwnerRejected, flowStatusStopped, flowStatusBlocked, flowStatusDeveloperFailed, flowStatusReviewerFailed:
		return "#c92a2a"
	case flowStatusReviewRunning, flowStatusDeveloperRunning, flowStatusFixRunning:
		return "#1c7ed6"
	default:
		return "#868e96"
	}
}

func newFlowActionToken() string {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

func pullRequestNumberFromURL(value string) int {
	matches := githubPRURLRE.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) != 2 {
		return 0
	}
	number, err := strconv.Atoi(matches[1])
	if err != nil || number <= 0 {
		return 0
	}
	return number
}

func repositoryChannelName(owner string, name string) string {
	raw := "repo-" + strings.ToLower(owner) + "-" + strings.ToLower(name)
	var builder strings.Builder
	lastDash := false
	for _, r := range raw {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(builder.String(), "-")
	if len(value) > 63 {
		value = strings.TrimRight(value[:63], "-")
	}
	if value == "" {
		return "repo-unknown"
	}
	return value
}

func (svc *SlashCommandService) recordGitHubAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "github",
		ResourceName: resourceName,
		Summary:      summary,
	})
}

func (svc *SlashCommandService) recordRuntimeAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "runtime",
		ResourceName: resourceName,
		Summary:      summary,
	})
}

func (svc *SlashCommandService) recordOpenAIAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "openai",
		ResourceName: resourceName,
		Summary:      summary,
	})
}

func (svc *SlashCommandService) recordPromptAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "prompt",
		ResourceName: resourceName,
		Summary:      summary,
	})
}

func (svc *SlashCommandService) recordFlowAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "flow",
		ResourceName: resourceName,
		Summary:      summary,
	})
}

func (svc *SlashCommandService) enabledProfile(ctx context.Context, name string) bool {
	profile, ok := svc.agentProfile(ctx, name)
	return ok && profile.Enabled
}

func (svc *SlashCommandService) agentProfile(ctx context.Context, name string) (entity.AgentProfile, bool) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.AgentProfile{}, false
	}
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		return entity.AgentProfile{}, false
	}
	for _, profile := range profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return entity.AgentProfile{}, false
}

func (svc *SlashCommandService) openAIAccount(ctx context.Context, name string) (entity.OpenAIAccount, bool) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.OpenAIAccount{}, false
	}
	account, err := svc.cfg.Store.GetOpenAIAccount(ctx, name)
	if err != nil {
		return entity.OpenAIAccount{}, false
	}
	return account, true
}

func (svc *SlashCommandService) githubAccount(ctx context.Context, name string) (entity.GitHubAccount, bool) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.GitHubAccount{}, false
	}
	account, err := svc.cfg.Store.GetGitHubAccount(ctx, name)
	if err != nil {
		return entity.GitHubAccount{}, false
	}
	return account, true
}

func promptGitHubData(accountName string) promptTemplateGitHubData {
	return promptTemplateGitHubData{
		Account:     defaultString(accountName, "primary"),
		TokenEnv:    "GH_TOKEN / GITHUB_TOKEN",
		UsernameEnv: "GITHUB_USERNAME / GITHUB_USER",
		EmailEnv:    "GITHUB_EMAIL",
	}
}

func (svc *SlashCommandService) promptTemplateLocaleData() promptTemplateLocaleData {
	return promptTemplateLocaleData{
		Code:     svc.cfg.Localizer.Locale(),
		Language: svc.t("prompt.template.language_name", nil),
	}
}

func (svc *SlashCommandService) agentRun(ctx context.Context, runID string) (entity.AgentRun, bool) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.AgentRun{}, false
	}
	run, err := svc.cfg.Store.GetAgentRun(ctx, runID)
	if err != nil {
		return entity.AgentRun{}, false
	}
	return run, true
}

func defaultDeveloperRunID() string {
	return fmt.Sprintf("dev-%d", time.Now().Unix())
}

func defaultReviewRunID() string {
	return fmt.Sprintf("review-%d", time.Now().Unix())
}

func developerSmokeBranch(runID string) string {
	return "matter-codex-dev-" + runID
}

func developerSmokeTask(runID string, ref repositoryRef) string {
	return fmt.Sprintf("Create or update docs/dogfood/codex-developer-smoke.md with a short Russian note that matter-codex developer smoke run `%s` for `%s/%s` reached the Codex developer agent stage. Keep the change limited to that file.", runID, ref.Owner, ref.Name)
}

func developerRunStatus(status runtimerepo.RunStatus) string {
	if status.Artifacts["pr-url"] != "" {
		return "pr_created"
	}
	if status.Artifacts["no-changes"] == "true" {
		return "completed_no_changes"
	}
	if status.JobFailed > 0 {
		return "failed"
	}
	if status.JobSucceeded > 0 {
		return "succeeded"
	}
	if status.JobActive > 0 {
		return "running"
	}
	return "pending"
}

func reviewerRunStatus(status runtimerepo.RunStatus) string {
	switch status.Artifacts["review-decision"] {
	case "approve":
		return "approved"
	case "request_changes":
		return "changes_requested"
	case "comment":
		return "review_comment"
	}
	if status.Artifacts["review-submitted"] == "true" {
		return "review_submitted"
	}
	if status.JobFailed > 0 {
		return "failed"
	}
	if status.JobSucceeded > 0 {
		return "succeeded"
	}
	if status.JobActive > 0 {
		return "running"
	}
	return "pending"
}

func openAICredentialName(accountName string) string {
	return "openai:" + accountName
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func codexAuthSecretName(baseName string, accountName string) string {
	baseName = strings.Trim(strings.TrimSpace(baseName), "-")
	if baseName == "" {
		baseName = "matter-codex-codex-auth"
	}
	accountName = strings.Trim(strings.TrimSpace(accountName), "-")
	if accountName == "" {
		accountName = "primary"
	}
	return baseName + "-" + accountName
}

type artifactPair struct {
	key   string
	value string
}

func sortedArtifacts(artifacts map[string]string) []artifactPair {
	if len(artifacts) == 0 {
		return nil
	}
	pairs := make([]artifactPair, 0, len(artifacts))
	for key, value := range artifacts {
		pairs = append(pairs, artifactPair{key: key, value: value})
	}
	sort.Slice(pairs, func(i int, j int) bool {
		return pairs[i].key < pairs[j].key
	})
	return pairs
}

func (svc *SlashCommandService) t(messageID string, data map[string]any) string {
	return svc.cfg.Localizer.T(messageID, data)
}

func supportedLocalesText(localizer *texti18n.Localizer) string {
	return strings.Join(localizer.SupportedLocales(), "`, `")
}

func shortSHA(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func defaultRuntimeRunID() string {
	return fmt.Sprintf("smoke-%d", time.Now().Unix())
}

func sanitizeLogTail(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "```", "'''")
}

func emptyAsUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "unknown error"
	}
	parts := strings.Split(text, "\n")
	sort.Strings(parts)
	return parts[0]
}
