package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
	EnsureProjectTeam(ctx context.Context, teamName string, displayName string, memberUserID string) (MattermostTeamBinding, bool, error)
	EnsureProjectChannel(ctx context.Context, teamName string, channelName string, displayName string, private bool, memberUserIDs []string) (MattermostChannelBinding, bool, error)
}

type FlowCardPublisher interface {
	UpsertFlowCard(ctx context.Context, card FlowCard) (FlowCardPost, error)
}

type MattermostTeamBinding struct {
	ID          string
	Name        string
	DisplayName string
}

type MattermostChannelBinding struct {
	ID          string
	TeamID      string
	Name        string
	DisplayName string
	Type        string
}

type MattermostCard struct {
	ChannelID string
	PostID    string
	ActionURL string
	Message   string
	Color     string
	Title     string
	Text      string
	Fields    []MattermostCardField
	Actions   []MattermostCardAction
}

type MattermostCardField struct {
	Title string
	Value string
	Short bool
}

type MattermostCardAction struct {
	ID       string
	Name     string
	Tooltip  string
	Style    string
	Disabled bool
	Context  map[string]any
}

type MattermostDialog struct {
	SubmitURL        string
	CallbackID       string
	Title            string
	IntroductionText string
	Elements         []MattermostDialogElement
	SubmitLabel      string
	State            string
}

type MattermostDialogElement struct {
	DisplayName string
	Name        string
	Type        string
	SubType     string
	Default     string
	Placeholder string
	HelpText    string
	Optional    bool
	MinLength   int
	MaxLength   int
	Options     []MattermostDialogOption
}

type MattermostDialogOption struct {
	Text  string
	Value string
}

type FlowCard = MattermostCard
type FlowCardField = MattermostCardField
type FlowCardAction = MattermostCardAction

type FlowCardPost struct {
	ChannelID string
	PostID    string
}

type SlashResponse struct {
	Text           string
	Card           *MattermostCard
	ChannelVisible bool
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

type MenuActionCommand struct {
	View      string
	Command   string
	Dialog    string
	Action    string
	Resource  string
	ID        string
	Page      int
	UserID    string
	UserName  string
	ChannelID string
	PostID    string
}

type MenuActionResult struct {
	Card          *MattermostCard
	Dialog        *MattermostDialog
	EphemeralText string
	StatusCode    int
}

type DialogSubmissionCommand struct {
	CallbackID string
	State      string
	UserID     string
	UserName   string
	ChannelID  string
	TeamID     string
	Submission map[string]any
	Cancelled  bool
}

type DialogSubmissionResult struct {
	Card       *MattermostCard
	Dialog     *MattermostDialog
	Error      string
	Errors     map[string]string
	StatusCode int
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
	Localizer                *texti18n.Localizer
	StatusService            *StatusService
	Store                    adminrepo.Repository
	ChannelManager           MattermostChannelManager
	RoleBotManager           MattermostRoleBotManager
	FlowCardPublisher        FlowCardPublisher
	RepositoryProvider       providerrepo.RepositoryProvider
	GitHubRepositoryProvider providerrepo.GitHubAccountRepositoryProvider
	GitHubAccountInspector   providerrepo.GitHubAccountInspector
	RuntimeRunner            runtimerepo.Runner
	DefaultTeamName          string
	CodexAuthSecretName      string
	GitHubSecretName         string
	MenuActionURL            string
	DialogSubmitURL          string
	FlowActionURL            string
	BotTokenConfigured       bool
	SlashTokenConfigured     bool
	GitHubTokenConfigured    bool
	GitHubWebhookConfigured  bool
	DatabaseConfigured       bool
	StorageReady             bool
	RuntimeConfigured        bool
	MattermostConfigured     bool
	ChannelManagerEnabled    bool
}

type SlashCommandService struct {
	cfg SlashCommandServiceConfig
}

func NewSlashCommandService(cfg SlashCommandServiceConfig) *SlashCommandService {
	return &SlashCommandService{cfg: cfg}
}

func (svc *SlashCommandService) Handle(ctx context.Context, command SlashCommand) string {
	return svc.HandleResponse(ctx, command).Text
}

func (svc *SlashCommandService) HandleResponse(ctx context.Context, command SlashCommand) SlashResponse {
	fields := strings.Fields(command.Text)
	if len(fields) == 0 {
		return SlashResponse{
			Text:           svc.t("menu.response", nil),
			Card:           svc.menuCard(ctx, menuViewMain),
			ChannelVisible: true,
		}
	}
	return SlashResponse{Text: svc.handleText(ctx, command, fields)}
}

func (svc *SlashCommandService) handleText(ctx context.Context, command SlashCommand, fields []string) string {
	if fields[0] == "status" {
		return svc.cfg.StatusService.SlashStatusText()
	}

	switch fields[0] {
	case "help":
		return svc.helpText()
	case "repo":
		return svc.handleRepo(ctx, fields[1:], command)
	case "project":
		return svc.handleProject(ctx, fields[1:], command)
	case "role":
		return svc.handleAgentRole(ctx, fields[1:])
	case "chat":
		return svc.handleChat(ctx, fields[1:])
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
	return svc.upsertRepositoryText(ctx, input, command, "repository metadata upserted from Mattermost slash command")
}

func (svc *SlashCommandService) upsertRepositoryText(ctx context.Context, input adminrepo.UpsertRepositoryInput, command SlashCommand, auditSummary string) string {
	_, text, _ := svc.upsertRepository(ctx, input, command, auditSummary)
	return text
}

func (svc *SlashCommandService) upsertRepository(ctx context.Context, input adminrepo.UpsertRepositoryInput, command SlashCommand, auditSummary string) (entity.Repository, string, bool) {
	input.GitHubAccountName = defaultString(input.GitHubAccountName, "primary")
	channelName := repositoryChannelName(input.Owner, input.Name)
	if svc.cfg.ChannelManager != nil {
		if _, err := svc.cfg.ChannelManager.EnsureRepositoryChannel(ctx, svc.cfg.DefaultTeamName, channelName, "repo "+input.Owner+"/"+input.Name); err != nil {
			return entity.Repository{}, svc.t("repo.add.channel_failed", map[string]any{"Error": safeError(err)}), false
		}
	}
	input.MattermostChannel = channelName
	repo, created, err := svc.cfg.Store.UpsertRepository(ctx, input)
	if err != nil {
		return entity.Repository{}, svc.t("repo.add.save_failed", map[string]any{"Error": safeError(err)}), false
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "repository.upserted",
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "repository",
		ResourceName: repo.Provider + ":" + repo.FullName(),
		Summary:      auditSummary,
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
			"Account":       repo.GitHubAccountName,
			"Channel":       repo.MattermostChannel,
			"DefaultBranch": repo.DefaultBranch,
		}),
	}
	lines = append(lines, svc.ensureRepositoryWebhookLineForRepository(ctx, command, repo)...)
	return repo, strings.Join(lines, "\n"), true
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
			"Account":       defaultString(repo.GitHubAccountName, "primary"),
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
		openAIAccounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
		if err == nil {
			authorized := 0
			for _, account := range openAIAccounts {
				if account.Status == "authorized" {
					authorized++
				}
			}
			lines = append(lines, svc.t("token.check.openai_accounts", map[string]any{"Authorized": authorized, "Total": len(openAIAccounts)}))
		}
		gitHubAccounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
		if err == nil {
			configured := 0
			for _, account := range gitHubAccounts {
				if account.Status == "configured" {
					configured++
				}
			}
			lines = append(lines, svc.t("token.check.github_accounts", map[string]any{"Configured": configured, "Total": len(gitHubAccounts)}))
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
	case "prune":
		return svc.handleRuntimePrune(ctx, args[1:], command)
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

func (svc *SlashCommandService) handleRuntimePrune(ctx context.Context, args []string, command SlashCommand) string {
	olderThan, dryRun, err := parseRuntimePruneArgs(args)
	if err != nil {
		return svc.t("runtime.prune.usage", nil)
	}
	result, err := svc.cfg.RuntimeRunner.CleanupExpiredRuns(ctx, runtimerepo.RetentionCleanupInput{
		OlderThan: olderThan,
		Now:       time.Now().UTC(),
		DryRun:    dryRun,
	})
	if err != nil {
		return svc.t("runtime.prune.failed", map[string]any{"Error": safeError(err)})
	}
	modeID := "runtime.prune.mode_dry_run"
	eventType := "runtime.retention.checked"
	eventSummary := "runtime retention cleanup dry-run requested from Mattermost slash command"
	if !result.DryRun {
		modeID = "runtime.prune.mode_apply"
		eventType = "runtime.retention.cleaned"
		eventSummary = "runtime retention cleanup applied from Mattermost slash command"
	}
	svc.recordRuntimeAudit(ctx, command, eventType, "agent-runner", eventSummary)
	return svc.t("runtime.prune.result", map[string]any{
		"Mode":              svc.t(modeID, nil),
		"OlderThan":         result.OlderThan.String(),
		"Namespace":         result.Namespace,
		"RunsMatched":       result.RunsMatched,
		"RunIDs":            formatRunIDList(result.MatchedRunIDs, 12),
		"SkippedActiveJobs": result.SkippedActiveJobs,
		"JobsMatched":       result.JobsMatched,
		"JobsDeleted":       result.JobsDeleted,
		"PVCsMatched":       result.PVCsMatched,
		"PVCsDeleted":       result.PVCsDeleted,
		"ConfigMapsMatched": result.ConfigMapsMatched,
		"ConfigMapsDeleted": result.ConfigMapsDeleted,
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
		FlowID:               flowID,
		Status:               flowStatusCreated,
		Provider:             "github",
		Owner:                ref.Owner,
		Name:                 ref.Name,
		BaseBranch:           "main",
		HeadBranch:           flowHeadBranch(flowID),
		Title:                title,
		Task:                 title,
		Attempt:              1,
		MaxAttempts:          defaultFlowMaxAttempts,
		DeveloperProfileName: "developer",
		ReviewerProfileName:  "reviewer",
		FlowPreset:           "developer_review",
		OwnerUserID:          strings.TrimSpace(command.UserID),
		OwnerUser:            strings.TrimSpace(command.UserName),
		ActionToken:          newFlowActionToken(),
		Summary:              "developer-review flow created from Mattermost slash command",
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
		{Title: svc.t("flow.card.field.developer_profile", nil), Value: defaultString(flow.DeveloperProfileName, "developer"), Short: true},
		{Title: svc.t("flow.card.field.reviewer_profile", nil), Value: defaultString(flow.ReviewerProfileName, "reviewer"), Short: true},
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
	profileName := defaultString(flow.DeveloperProfileName, "developer")
	account, message, ok := svc.flowAgentRuntime(ctx, profileName)
	if !ok {
		return flow, runtimerepo.StartedRun{}, errors.New(message)
	}
	role := defaultString(account.Profile.Role, "developer")
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
	prompt, err := svc.renderStoredPromptTemplate(ctx, profileName, templateKey, promptTemplateData{
		Run: promptTemplateRunData{
			ID:      runID,
			Profile: profileName,
			Role:    role,
			Locale:  svc.cfg.Localizer.Locale(),
		},
		Agent: promptTemplateAgentData{
			Profile:          profileName,
			Role:             role,
			KubernetesAccess: defaultString(account.Profile.KubernetesAccess, "read-only"),
			SandboxMode:      defaultString(account.Profile.SandboxMode, "danger-full-access"),
			ConfigOverlay:    account.Profile.ConfigOverlay,
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
		Profile:             profileName,
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
		ProfileName:         profileName,
		Role:                role,
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
	profileName := defaultString(flow.ReviewerProfileName, "reviewer")
	account, message, ok := svc.flowAgentRuntime(ctx, profileName)
	if !ok {
		return flow, runtimerepo.StartedRun{}, errors.New(message)
	}
	role := defaultString(account.Profile.Role, "reviewer")
	prompt, err := svc.renderStoredPromptTemplate(ctx, profileName, reviewPRTemplateKey, promptTemplateData{
		Run: promptTemplateRunData{
			ID:      runID,
			Profile: profileName,
			Role:    role,
			Locale:  svc.cfg.Localizer.Locale(),
		},
		Agent: promptTemplateAgentData{
			Profile:          profileName,
			Role:             role,
			KubernetesAccess: defaultString(account.Profile.KubernetesAccess, "read-only"),
			SandboxMode:      defaultString(account.Profile.SandboxMode, "danger-full-access"),
			ConfigOverlay:    account.Profile.ConfigOverlay,
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
		Profile:             profileName,
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
		ProfileName:         profileName,
		Role:                role,
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
	case flowStatusHeld:
		return svc.t("flow.summary.held", nil)
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
	case "delete":
		return svc.handleOpenAIDelete(ctx, args[1:], command)
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

func (svc *SlashCommandService) handleOpenAIDelete(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	if len(args) != 1 {
		return svc.t("openai.delete.usage", nil)
	}
	accountName, err := parseOpenAIAccountName(args[0])
	if err != nil {
		return svc.validationErrorText(err)
	}
	account, err := svc.cfg.Store.GetOpenAIAccount(ctx, accountName)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return svc.t("openai.status.account_not_found", map[string]any{"Account": accountName})
		}
		return svc.t("openai.delete.failed", map[string]any{"Error": safeError(err)})
	}
	profiles, err := svc.openAIAccountProfileRefs(ctx, account.Name)
	if err != nil {
		return svc.t("openai.delete.profile_check_failed", map[string]any{"Error": safeError(err)})
	}
	if len(profiles) > 0 {
		return svc.t("openai.delete.in_use", map[string]any{"Account": account.Name, "Profiles": strings.Join(profiles, ", ")})
	}
	deletedRuntime, err := svc.cfg.RuntimeRunner.DeleteCodexAuthAccount(ctx, account.Name, account.SecretRef)
	if err != nil {
		return svc.t("openai.delete.failed", map[string]any{"Error": safeError(err)})
	}
	deletedAccount, err := svc.cfg.Store.DeleteOpenAIAccount(ctx, account.Name)
	if err != nil {
		return svc.t("openai.delete.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordOpenAIAudit(ctx, command, "openai.account.deleted", deletedAccount.Name, "openai account metadata and auth secret deleted from Mattermost command")
	return svc.t("openai.delete.result", map[string]any{
		"Account":       deletedAccount.Name,
		"Secret":        deletedAccount.SecretRef,
		"Namespace":     deletedRuntime.Namespace,
		"JobDeleted":    deletedRuntime.JobDeleted,
		"SecretDeleted": deletedRuntime.SecretDeleted,
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

func (svc *SlashCommandService) HandleMenuAction(ctx context.Context, command MenuActionCommand) MenuActionResult {
	view := normalizeMenuView(command.View)
	card := svc.menuCard(ctx, view)
	card.ChannelID = strings.TrimSpace(command.ChannelID)
	card.PostID = strings.TrimSpace(command.PostID)
	if action := strings.TrimSpace(command.Action); action != "" {
		return svc.handleMenuTypedAction(ctx, command, card)
	}
	if dialogID := strings.TrimSpace(command.Dialog); dialogID != "" {
		dialog, errText := svc.menuDialog(ctx, command, dialogID)
		if errText != "" {
			return MenuActionResult{StatusCode: 200, EphemeralText: errText, Card: card}
		}
		return MenuActionResult{
			StatusCode:    200,
			EphemeralText: svc.t("dialog.opened", map[string]any{"Title": dialog.Title}),
			Dialog:        dialog,
		}
	}
	if textCommand := strings.TrimSpace(command.Command); textCommand != "" {
		fields := strings.Fields(textCommand)
		if len(fields) == 0 {
			return MenuActionResult{StatusCode: 200, EphemeralText: svc.t("slash.unknown_command", nil), Card: card}
		}
		text := svc.handleText(ctx, SlashCommand{
			Text:      textCommand,
			UserID:    command.UserID,
			UserName:  command.UserName,
			ChannelID: command.ChannelID,
		}, fields)
		card = svc.menuCommandResultCard(ctx, view, textCommand, text)
		card.ChannelID = strings.TrimSpace(command.ChannelID)
		card.PostID = strings.TrimSpace(command.PostID)
		return MenuActionResult{
			StatusCode:    200,
			EphemeralText: text,
			Card:          card,
		}
	}
	return MenuActionResult{
		StatusCode:    200,
		EphemeralText: svc.t("menu.action.opened", map[string]any{"Title": card.Title}),
		Card:          card,
	}
}

func (svc *SlashCommandService) handleMenuTypedAction(ctx context.Context, command MenuActionCommand, currentCard *MattermostCard) MenuActionResult {
	action := strings.TrimSpace(command.Action)
	resource := strings.TrimSpace(command.Resource)
	switch action {
	case menuActionList:
		switch resource {
		case menuResourceProject:
			return svc.menuCardResult(command, svc.projectListCard(ctx, command))
		case menuResourceRepository:
			return svc.menuCardResult(command, svc.repositoryListCard(ctx, command))
		case menuResourceAgentRole:
			return svc.menuCardResult(command, svc.roleListCard(ctx, command))
		case menuResourceChat:
			return svc.menuCardResult(command, svc.chatListCard(ctx, command))
		case menuResourceOpenAIAccount:
			return svc.menuCardResult(command, svc.openAIAccountListCard(ctx, command))
		case menuResourceGitHubAccount:
			return svc.menuCardResult(command, svc.githubAccountListCard(ctx, command))
		case menuResourceProfile:
			return svc.menuCardResult(command, svc.profileListCard(ctx, command))
		case menuResourcePromptTemplate:
			return svc.menuCardResult(command, svc.promptTemplateListCard(ctx, command))
		case menuResourceFlow:
			return svc.menuCardResult(command, svc.flowListCard(ctx, command, false))
		case menuResourceRun:
			return svc.menuCardResult(command, svc.runtimeRunListCard(ctx, command))
		default:
			return svc.menuActionTextResult(ctx, command, svc.t("menu.action.unknown", nil), false)
		}
	case menuActionShow:
		switch resource {
		case menuResourceProject:
			return svc.menuCardResult(command, svc.projectEntityCard(ctx, command))
		case menuResourceRepository:
			return svc.menuCardResult(command, svc.repositoryEntityCard(ctx, command))
		case menuResourceAgentRole:
			return svc.menuCardResult(command, svc.roleEntityCard(ctx, command))
		case menuResourceChat:
			return svc.menuCardResult(command, svc.chatEntityCard(ctx, command))
		case menuResourceOpenAIAccount:
			return svc.menuCardResult(command, svc.openAIAccountEntityCard(ctx, command))
		case menuResourceGitHubAccount:
			return svc.menuCardResult(command, svc.githubAccountEntityCard(ctx, command))
		case menuResourceProfile:
			return svc.menuCardResult(command, svc.profileEntityCard(ctx, command))
		case menuResourcePromptTemplate:
			return svc.menuCardResult(command, svc.promptTemplateEntityCard(ctx, command))
		case menuResourceFlow:
			return svc.menuCardResult(command, svc.flowEntityCard(ctx, command))
		case menuResourceRun:
			return svc.menuCardResult(command, svc.runtimeRunEntityCard(ctx, command))
		default:
			return svc.menuActionTextResult(ctx, command, svc.t("menu.action.unknown", nil), false)
		}
	case menuActionConfirmDelete:
		switch resource {
		case menuResourceRepository:
			return svc.menuCardResult(command, svc.repositoryDeleteConfirmationCard(ctx, command))
		case menuResourceOpenAIAccount:
			return svc.menuCardResult(command, svc.openAIAccountDeleteConfirmationCard(ctx, command))
		case menuResourceGitHubAccount:
			return svc.menuCardResult(command, svc.githubAccountDeleteConfirmationCard(ctx, command))
		default:
			return svc.menuActionTextResult(ctx, command, svc.t("menu.action.unknown", nil), false)
		}
	case menuActionDelete:
		switch resource {
		case menuResourceRepository:
			return svc.menuActionTextResult(ctx, command, svc.deleteRepositoryFromMenu(ctx, command), false)
		case menuResourceOpenAIAccount:
			return svc.menuActionTextResult(ctx, command, svc.handleOpenAIDelete(ctx, []string{command.ID}, svc.slashFromMenu(command)), false)
		case menuResourceGitHubAccount:
			return svc.menuActionTextResult(ctx, command, svc.deleteGitHubAccountFromMenu(ctx, command), false)
		default:
			return svc.menuActionTextResult(ctx, command, svc.t("menu.action.unknown", nil), false)
		}
	case menuActionCancel:
		return svc.menuCardResult(command, currentCard)
	case menuActionRepositoryOnboard:
		return svc.menuCardResult(command, svc.repositoryOnboardingEntryCard(ctx, command))
	case menuActionRepositoryRepos:
		return svc.menuCardResult(command, svc.repositoryOnboardingRepositoryCard(ctx, command, strings.TrimSpace(command.ID), ""))
	case menuActionRepositoryBranches:
		return svc.menuCardResult(command, svc.repositoryOnboardingBranchCard(ctx, command))
	case menuActionRepositoryConnect:
		return svc.menuCardResult(command, svc.repositoryOnboardingConnectCard(ctx, command))
	case menuActionRepositoryCheck:
		provider, owner, name, ok := parseRepositoryResourceID(command.ID)
		if !ok || provider != "github" {
			return svc.menuActionTextResult(ctx, command, svc.t("menu.entity.invalid", nil), false)
		}
		text := svc.repositoryGitHubCheckText(ctx, command, provider, owner, name)
		return svc.menuActionTextResult(ctx, command, text, false)
	case menuActionRepositoryWebhook:
		provider, owner, name, ok := parseRepositoryResourceID(command.ID)
		if !ok || provider != "github" {
			return svc.menuActionTextResult(ctx, command, svc.t("menu.entity.invalid", nil), false)
		}
		text := svc.repositoryGitHubWebhookText(ctx, command, provider, owner, name)
		return svc.menuActionTextResult(ctx, command, text, false)
	case menuActionOpenAIStatus:
		text := svc.handleOpenAIStatus(ctx, []string{command.ID}, svc.slashFromMenu(command))
		return svc.menuOpenAIAccountResult(ctx, command, text)
	case menuActionOpenAIAuth:
		text := svc.handleOpenAIAuth(ctx, []string{command.ID}, svc.slashFromMenu(command))
		return svc.menuOpenAIAccountResult(ctx, command, text)
	case menuActionOpenAICleanup:
		text := svc.handleOpenAICleanup(ctx, []string{command.ID}, svc.slashFromMenu(command))
		return svc.menuActionTextResult(ctx, command, text, false)
	case menuActionSystemStatus:
		return svc.menuActionTextResult(ctx, command, svc.cfg.StatusService.SlashStatusText(), false)
	case menuActionTokenCheck:
		return svc.menuActionTextResult(ctx, command, svc.handleToken(ctx, []string{"check"}), false)
	case menuActionLocaleGet:
		return svc.menuActionTextResult(ctx, command, svc.handleLocale([]string{"get"}), false)
	case menuActionLocaleSetRU:
		return svc.menuActionTextResult(ctx, command, svc.handleLocale([]string{"set", "ru"}), false)
	case menuActionLocaleSetEN:
		return svc.menuActionTextResult(ctx, command, svc.handleLocale([]string{"set", "en"}), false)
	case menuActionRuntimeSmoke:
		return svc.menuActionTextResult(ctx, command, svc.handleRuntimeSmoke(ctx, nil, svc.slashFromMenu(command)), false)
	case menuActionRuntimePruneDry:
		return svc.menuActionTextResult(ctx, command, svc.handleRuntimePrune(ctx, []string{"24h"}, svc.slashFromMenu(command)), false)
	case menuActionRuntimeCleanup:
		return svc.menuActionTextResult(ctx, command, svc.handleRuntimeCleanup(ctx, []string{command.ID}, svc.slashFromMenu(command)), false)
	case menuActionPromptHelp:
		profileName, templateKey, ok := parsePromptTemplateResourceID(command.ID)
		if !ok {
			return svc.menuActionTextResult(ctx, command, svc.t("menu.entity.invalid", nil), false)
		}
		return svc.menuActionTextResult(ctx, command, svc.handlePromptHelp(profileName+" "+templateKey), false)
	case menuActionPromptRender:
		profileName, templateKey, ok := parsePromptTemplateResourceID(command.ID)
		if !ok {
			return svc.menuActionTextResult(ctx, command, svc.t("menu.entity.invalid", nil), false)
		}
		return svc.menuActionTextResult(ctx, command, svc.handlePromptRender(ctx, profileName+" "+templateKey), false)
	case menuActionProfileEnable:
		return svc.menuCardResult(command, svc.updateProfileEnabledCard(ctx, command, true))
	case menuActionProfileDisable:
		return svc.menuCardResult(command, svc.updateProfileEnabledCard(ctx, command, false))
	case menuActionFlowAdvance:
		return svc.menuCardResult(command, svc.advanceFlowCard(ctx, command))
	case menuActionFlowCard:
		return svc.menuActionTextResult(ctx, command, svc.handleFlowCard(ctx, []string{command.ID}, svc.slashFromMenu(command)), false)
	case menuActionFlowCleanup:
		return svc.menuActionTextResult(ctx, command, svc.handleFlowCleanup(ctx, []string{command.ID}, svc.slashFromMenu(command)), false)
	case menuActionFlowApprove:
		return svc.menuFlowActionResult(ctx, command, flowActionApprove)
	case menuActionFlowReject:
		return svc.menuFlowActionResult(ctx, command, flowActionReject)
	case menuActionFlowRerun:
		return svc.menuFlowActionResult(ctx, command, flowActionRerun)
	case menuActionFlowStop:
		return svc.menuFlowActionResult(ctx, command, flowActionStop)
	case menuActionFlowHold:
		return svc.menuCardResult(command, svc.updateFlowHoldCard(ctx, command, true))
	case menuActionFlowResume:
		return svc.menuCardResult(command, svc.updateFlowHoldCard(ctx, command, false))
	default:
		return svc.menuActionTextResult(ctx, command, svc.t("menu.action.unknown", nil), false)
	}
}

func (svc *SlashCommandService) menuCardResult(command MenuActionCommand, card *MattermostCard) MenuActionResult {
	applyMenuCardIdentity(card, command)
	return MenuActionResult{
		StatusCode:    200,
		EphemeralText: svc.t("menu.action.opened", map[string]any{"Title": card.Title}),
		Card:          card,
	}
}

func (svc *SlashCommandService) menuActionTextResult(ctx context.Context, command MenuActionCommand, text string, private bool) MenuActionResult {
	view := normalizeMenuView(command.View)
	card := svc.menuCommandResultCard(ctx, view, "", text)
	if private {
		card.Text = svc.t("menu.command_result.private_text", nil)
	}
	applyMenuCardIdentity(card, command)
	return MenuActionResult{
		StatusCode:    200,
		EphemeralText: text,
		Card:          card,
	}
}

func (svc *SlashCommandService) menuOpenAIAccountResult(ctx context.Context, command MenuActionCommand, text string) MenuActionResult {
	card := svc.menuCommandResultCard(ctx, menuViewOpenAI, "", text)
	accountName := strings.TrimSpace(command.ID)
	if accountName != "" {
		card.Actions = []MattermostCardAction{
			svc.menuResourceAction(menuViewOpenAI, "openaistatus", menuActionOpenAIStatus, menuResourceOpenAIAccount, accountName, "menu.action.openai_status", "menu.action.openai_status.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewOpenAI, "openaiauthrestart", menuActionOpenAIAuth, menuResourceOpenAIAccount, accountName, "menu.action.openai_auth_restart", "menu.action.openai_auth_restart.tooltip", "warning", nil),
			svc.menuResourceAction(menuViewOpenAI, "openaiaccount", menuActionShow, menuResourceOpenAIAccount, accountName, "menu.action.openai_account_open", "menu.action.openai_account_open.tooltip", "default", nil),
			svc.menuResourceAction(menuViewOpenAI, "openailist", menuActionList, menuResourceOpenAIAccount, "", "menu.action.openai_list", "menu.action.openai_list.tooltip", "default", nil),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		}
	}
	applyMenuCardIdentity(card, command)
	return MenuActionResult{
		StatusCode: 200,
		Card:       card,
	}
}

func (svc *SlashCommandService) repositoryListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRepositories)
	card.Title = svc.t("menu.entity.repositories.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("repo.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewRepositories)
		return card
	}
	repositories, err := svc.cfg.Store.ListRepositories(ctx, 100)
	if err != nil {
		card.Text = svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewRepositories)
		return card
	}
	sort.Slice(repositories, func(i int, j int) bool {
		return repositories[i].Provider+":"+repositories[i].FullName() < repositories[j].Provider+":"+repositories[j].FullName()
	})
	card.Text = svc.entityListText(len(repositories), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(repositories) == 0 {
		card.Text = svc.t("repo.list.empty", nil)
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewRepositories, "repoonboard", menuActionRepositoryOnboard, menuResourceRepository, "", "menu.action.repo_add", "menu.action.repo_add.tooltip", "primary", nil))
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewRepositories)...)
		return card
	}
	start, end, page := entityPageBounds(len(repositories), command.Page)
	for idx, repo := range repositories[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.repository.title", map[string]any{"Number": number, "Provider": repo.Provider, "FullName": repo.FullName()}),
			Value: svc.t("menu.entity.repository.summary", map[string]any{
				"Branch":  repo.DefaultBranch,
				"Account": defaultString(repo.GitHubAccountName, "primary"),
				"Channel": repo.MattermostChannel,
				"Status":  repo.Status,
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewRepositories, "openrepo"+strconv.Itoa(number), menuActionShow, menuResourceRepository, repositoryResourceID(repo), "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewRepositories, menuResourceRepository, "", page, len(repositories))...)
	card.Actions = append(card.Actions, svc.menuResourceAction(menuViewRepositories, "repoonboard", menuActionRepositoryOnboard, menuResourceRepository, "", "menu.action.repo_add", "menu.action.repo_add.tooltip", "primary", nil))
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewRepositories)...)
	return card
}

func (svc *SlashCommandService) repositoryEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRepositories)
	card.Title = svc.t("menu.entity.repository.card_title", nil)
	provider, owner, name, ok := parseRepositoryResourceID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewRepositories)
		return card
	}
	repo, err := svc.cfg.Store.GetRepository(ctx, provider, owner, name)
	if err != nil {
		card.Text = svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewRepositories)
		return card
	}
	card.Text = svc.t("menu.entity.repository.card_text", map[string]any{"Provider": repo.Provider, "FullName": repo.FullName()})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.repository", nil), Value: "`" + repo.Provider + ":" + repo.FullName() + "`", Short: true},
		{Title: svc.t("menu.entity.field.branch", nil), Value: "`" + repo.DefaultBranch + "`", Short: true},
		{Title: svc.t("menu.entity.field.github", nil), Value: "`" + defaultString(repo.GitHubAccountName, "primary") + "`", Short: true},
		{Title: svc.t("menu.entity.field.channel", nil), Value: "`" + repo.MattermostChannel + "`", Short: true},
		{Title: svc.t("menu.entity.field.status", nil), Value: "`" + repo.Status + "`", Short: true},
	}
	resourceID := repositoryResourceID(repo)
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewRepositories, "repoaccess", menuActionRepositoryCheck, menuResourceRepository, resourceID, "menu.action.repository_check", "menu.action.repository_check.tooltip", "default", nil),
		svc.menuResourceAction(menuViewRepositories, "repowebhook", menuActionRepositoryWebhook, menuResourceRepository, resourceID, "menu.action.repository_webhook", "menu.action.repository_webhook.tooltip", "default", nil),
		svc.menuResourceAction(menuViewRepositories, "repodeleteconfirm", menuActionConfirmDelete, menuResourceRepository, resourceID, "menu.action.repo_delete", "menu.action.repo_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewRepositories, "repolist", menuActionList, menuResourceRepository, "", "menu.action.repo_list", "menu.action.repo_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) repositoryDeleteConfirmationCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRepositories)
	card.Title = svc.t("menu.confirm.repository_delete.title", nil)
	card.Text = svc.t("menu.confirm.repository_delete.text", map[string]any{"Repository": command.ID})
	card.Fields = []MattermostCardField{{Title: svc.t("menu.entity.field.repository", nil), Value: "`" + command.ID + "`", Short: false}}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewRepositories, "repodelete", menuActionDelete, menuResourceRepository, command.ID, "menu.action.confirm_delete", "menu.action.confirm_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewRepositories, "repocancel", menuActionShow, menuResourceRepository, command.ID, "menu.action.cancel", "menu.action.cancel.tooltip", "default", nil),
	}
	return card
}

func (svc *SlashCommandService) deleteRepositoryFromMenu(ctx context.Context, command MenuActionCommand) string {
	provider, owner, name, ok := parseRepositoryResourceID(command.ID)
	if !ok {
		return svc.t("menu.entity.invalid", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("repo.list.storage_not_ready", nil)
	}
	deleted, err := svc.cfg.Store.DeleteRepository(ctx, provider, owner, name)
	if err != nil {
		return svc.t("repo.delete.failed", map[string]any{"Error": safeError(err)})
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "repository.deleted",
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "repository",
		ResourceName: deleted.Provider + ":" + deleted.FullName(),
		Summary:      "repository metadata deleted from Mattermost entity card",
	})
	return svc.t("repo.delete.result", map[string]any{"Provider": deleted.Provider, "FullName": deleted.FullName()})
}

func (svc *SlashCommandService) repositoryOnboardingAccountCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRepositories)
	card.Title = svc.t("repo.onboard.accounts.title", nil)
	card.Text = svc.t("repo.onboard.accounts.text", nil)
	card.Fields = nil
	card.Actions = nil
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("github.account.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewRepositories)
		return card
	}
	accounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		card.Text = svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewRepositories)
		return card
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	if len(accounts) == 0 {
		card.Text = svc.t("repo.onboard.accounts.empty", nil)
		card.Actions = append(card.Actions,
			svc.menuDialogAction(menuViewGitHub, "dialoggithubadd", menuDialogGitHubAccountAdd, "menu.action.github_account_add", "menu.action.github_account_add.tooltip", "primary"),
			svc.menuAction(menuViewRepositories, "menu.action.back", "menu.action.back.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		)
		return card
	}
	for idx, account := range accounts {
		number := idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("repo.onboard.account.title", map[string]any{"Number": number, "Account": account.Name}),
			Value: svc.t("repo.onboard.account.summary", map[string]any{
				"Status":   account.Status,
				"Username": emptyAsUnknown(account.Username),
				"Email":    emptyAsUnknown(account.Email),
			}),
			Short: false,
		})
		action := svc.menuResourceAction(menuViewRepositories, "repoaccount"+strconv.Itoa(number), menuActionRepositoryRepos, menuResourceGitHubAccount, account.Name, "menu.action.repository_choose_account", "menu.action.repository_choose_account.tooltip", "default", map[string]any{"Account": account.Name})
		if account.Status != "configured" || strings.TrimSpace(account.SecretRef) == "" {
			action.Disabled = true
		}
		card.Actions = append(card.Actions, action)
	}
	card.Actions = append(card.Actions,
		svc.menuDialogAction(menuViewGitHub, "dialoggithubadd", menuDialogGitHubAccountAdd, "menu.action.github_account_add", "menu.action.github_account_add.tooltip", "default"),
		svc.menuAction(menuViewRepositories, "menu.action.back", "menu.action.back.tooltip", "default"),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	)
	return card
}

func (svc *SlashCommandService) repositoryOnboardingEntryCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	if command.Resource != menuResourceProject || strings.TrimSpace(command.ID) == "" {
		return svc.repositoryOnboardingAccountCard(ctx, command)
	}
	projectID, ok := parseInt64ID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card := svc.menuCommandResultCard(ctx, menuViewProjects, "", svc.t("menu.entity.invalid", nil))
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	project, err := svc.cfg.Store.GetProject(ctx, projectID)
	if err != nil {
		card := svc.menuCommandResultCard(ctx, menuViewProjects, "", svc.t("project.get.failed", map[string]any{"Error": safeError(err)}))
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	accountName := strings.TrimSpace(project.GitHubAccountName)
	if accountName == "" {
		card := svc.menuCommandResultCard(ctx, menuViewProjects, "", svc.t("project.github_account.required", map[string]any{"Project": project.Name}))
		card.Actions = []MattermostCardAction{
			svc.menuResourceDialogAction(menuViewProjects, "dialogprojectedit", menuDialogProjectUpsert, menuResourceProject, strconv.FormatInt(project.ID, 10), "menu.action.project_edit", "menu.action.project_edit.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(project.ID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		}
		return card
	}
	return svc.repositoryOnboardingRepositoryCard(ctx, command, accountName, "")
}

func (svc *SlashCommandService) repositoryOnboardingRepositoryCard(ctx context.Context, command MenuActionCommand, accountName string, query string) *MattermostCard {
	view := normalizeMenuView(defaultString(command.View, menuViewRepositories))
	command.View = view
	card := svc.menuCard(ctx, view)
	card.Title = svc.t("repo.onboard.repositories.title", map[string]any{"Account": accountName})
	card.Text = svc.t("repo.onboard.repositories.text", map[string]any{"Account": accountName})
	card.Fields = nil
	card.Actions = nil
	account, ok, errText := svc.repositoryOnboardingAccount(ctx, accountName)
	if errText != "" {
		card.Text = errText
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	if !ok {
		card.Text = svc.t("repo.account.missing", map[string]any{"Account": accountName})
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	if svc.cfg.GitHubRepositoryProvider == nil {
		card.Text = svc.t("repo.onboard.provider_not_configured", nil)
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	candidates, err := svc.repositoryCandidates(ctx, account, query)
	if err != nil {
		card.Text = svc.t("repo.onboard.repositories.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
		return card
	}
	if strings.TrimSpace(query) != "" {
		card.Text = svc.t("repo.onboard.repositories.search_text", map[string]any{"Account": account.Name, "Query": query})
	}
	if len(candidates) == 0 {
		card.Text = svc.t("repo.onboard.repositories.empty", map[string]any{"Account": account.Name})
		card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
		return card
	}
	limit := len(candidates)
	if limit > entityListPageSize {
		limit = entityListPageSize
	}
	for idx, candidate := range candidates[:limit] {
		number := idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("repo.onboard.repository.title", map[string]any{"Number": number, "FullName": candidate.FullName}),
			Value: svc.t("repo.onboard.repository.summary", map[string]any{
				"Branch":      emptyAsUnknown(candidate.DefaultBranch),
				"Private":     candidate.Private,
				"Description": emptyAsUnknown(candidate.Description),
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(command.View, "repocandidate"+strconv.Itoa(number), menuActionRepositoryBranches, menuResourceRepository, repositoryOnboardingResourceID(repositoryOnboardingState{
			ProjectID:     repositoryOnboardingProjectID(command),
			Account:       account.Name,
			Provider:      candidate.Provider,
			Owner:         candidate.Owner,
			Name:          candidate.Name,
			FullName:      candidate.FullName,
			DefaultBranch: candidate.DefaultBranch,
		}), "menu.action.repository_choose", "menu.action.repository_choose.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.repositoryOnboardingRepositoryNavigation(command, account.Name)...)
	return card
}

func (svc *SlashCommandService) repositoryOnboardingBranchCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	view := normalizeMenuView(defaultString(command.View, menuViewRepositories))
	command.View = view
	card := svc.menuCard(ctx, view)
	card.Title = svc.t("repo.onboard.branches.title", nil)
	card.Fields = nil
	card.Actions = nil
	state, ok := parseRepositoryOnboardingResourceID(command.ID)
	if !ok || state.Account == "" || state.Owner == "" || state.Name == "" {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	account, ok, errText := svc.repositoryOnboardingAccount(ctx, state.Account)
	if errText != "" {
		card.Text = errText
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	if !ok {
		card.Text = svc.t("repo.account.missing", map[string]any{"Account": state.Account})
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	if svc.cfg.GitHubRepositoryProvider == nil {
		card.Text = svc.t("repo.onboard.provider_not_configured", nil)
		card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
		return card
	}
	branches, err := svc.cfg.GitHubRepositoryProvider.ListBranches(ctx, gitHubAccountRef(account), state.Owner, state.Name, 20)
	if err != nil {
		card.Text = svc.t("repo.onboard.branches.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
		return card
	}
	branchNames := repositoryOnboardingBranchNames(state.DefaultBranch, branches)
	if len(branchNames) == 0 {
		card.Text = svc.t("repo.onboard.branches.empty", map[string]any{"Repository": state.FullName})
		card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
		return card
	}
	card.Text = svc.t("repo.onboard.branches.text", map[string]any{"Repository": state.FullName, "Account": account.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.repository", nil), Value: "`" + state.Provider + ":" + state.FullName + "`", Short: true},
		{Title: svc.t("menu.entity.field.github", nil), Value: "`" + account.Name + "`", Short: true},
	}
	limit := len(branchNames)
	if limit > entityListPageSize {
		limit = entityListPageSize
	}
	for idx, branch := range branchNames[:limit] {
		next := state
		next.Branch = branch
		number := idx + 1
		card.Actions = append(card.Actions, svc.menuResourceAction(command.View, "repobranch"+strconv.Itoa(number), menuActionRepositoryConnect, menuResourceRepository, repositoryOnboardingResourceID(next), "menu.action.repository_connect_branch", "menu.action.repository_connect_branch.tooltip", "primary", map[string]any{"Branch": branch}))
	}
	card.Actions = append(card.Actions, svc.repositoryOnboardingRepositoryNavigation(command, account.Name)...)
	return card
}

func (svc *SlashCommandService) repositoryOnboardingConnectCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	view := normalizeMenuView(defaultString(command.View, menuViewRepositories))
	command.View = view
	card := svc.menuCommandResultCard(ctx, view, "", svc.t("menu.entity.invalid", nil))
	state, ok := parseRepositoryOnboardingResourceID(command.ID)
	if !ok || state.Account == "" || state.Owner == "" || state.Name == "" || state.Branch == "" {
		return card
	}
	account, ok, errText := svc.repositoryOnboardingAccount(ctx, state.Account)
	if errText != "" {
		card.Text = errText
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	if !ok {
		card.Text = svc.t("repo.account.missing", map[string]any{"Account": state.Account})
		card.Actions = svc.repositoryOnboardingAccountNavigation(command)
		return card
	}
	repo, text, saved := svc.upsertRepository(ctx, adminrepo.UpsertRepositoryInput{
		Provider:          defaultString(state.Provider, "github"),
		Owner:             state.Owner,
		Name:              state.Name,
		DefaultBranch:     state.Branch,
		GitHubAccountName: account.Name,
	}, svc.slashFromMenu(command), "repository onboarded from Mattermost menu")
	if saved && state.ProjectID > 0 {
		binding, created, err := svc.cfg.Store.UpsertProjectRepository(ctx, adminrepo.UpsertProjectRepositoryInput{
			ProjectID:    state.ProjectID,
			RepositoryID: repo.ID,
			IsDefault:    true,
		})
		if err != nil {
			card.Text = text + "\n" + svc.t("project_repo.save.failed", map[string]any{"Error": safeError(err)})
			card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
			return card
		}
		stateID := "label.updated"
		if created {
			stateID = "label.created"
		}
		text += "\n" + svc.t("project_repo.save.result", map[string]any{
			"State":      svc.t(stateID, nil),
			"Repository": binding.Provider + ":" + binding.FullName(),
			"Project":    state.ProjectID,
			"Default":    binding.IsDefault,
		})
	}
	card.Text = text
	if saved {
		card.Actions = []MattermostCardAction{
			svc.menuResourceAction(menuViewRepositories, "openrepo", menuActionShow, menuResourceRepository, repositoryResourceID(repo), "menu.action.repository_open", "menu.action.repository_open.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewRepositories, "repolist", menuActionList, menuResourceRepository, "", "menu.action.repo_list", "menu.action.repo_list.tooltip", "default", nil),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		}
		if state.ProjectID > 0 {
			card.Actions = append([]MattermostCardAction{
				svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(state.ProjectID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "primary", nil),
			}, card.Actions...)
		}
		return card
	}
	card.Actions = svc.repositoryOnboardingRepositoryNavigation(command, account.Name)
	return card
}

func (svc *SlashCommandService) repositoryCandidates(ctx context.Context, account entity.GitHubAccount, query string) ([]providerrepo.RepositoryCandidate, error) {
	if strings.TrimSpace(query) != "" {
		return svc.cfg.GitHubRepositoryProvider.SearchRepositories(ctx, providerrepo.RepositorySearchInput{
			Account: gitHubAccountRef(account),
			Query:   query,
			Limit:   10,
		})
	}
	return svc.cfg.GitHubRepositoryProvider.ListRepositories(ctx, providerrepo.RepositoryListInput{
		Account: gitHubAccountRef(account),
		Limit:   10,
	})
}

func (svc *SlashCommandService) repositoryOnboardingAccount(ctx context.Context, accountName string) (entity.GitHubAccount, bool, string) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return entity.GitHubAccount{}, false, svc.t("repo.onboard.account_required", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.GitHubAccount{}, false, svc.t("github.account.list.storage_not_ready", nil)
	}
	account, err := svc.cfg.Store.GetGitHubAccount(ctx, accountName)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return entity.GitHubAccount{}, false, ""
		}
		return entity.GitHubAccount{}, false, svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
	}
	if account.Status != "configured" || strings.TrimSpace(account.SecretRef) == "" {
		return entity.GitHubAccount{}, false, svc.t("repo.onboard.account_not_configured", map[string]any{"Account": account.Name})
	}
	return account, true, ""
}

func (svc *SlashCommandService) repositoryOnboardingAccountNavigation(command MenuActionCommand) []MattermostCardAction {
	if projectID := repositoryOnboardingProjectID(command); projectID > 0 {
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(projectID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		}
	}
	return []MattermostCardAction{
		svc.menuResourceAction(menuViewRepositories, "repoonboard", menuActionRepositoryOnboard, menuResourceRepository, "", "menu.action.repo_add", "menu.action.repo_add.tooltip", "default", nil),
		svc.menuAction(menuViewRepositories, "menu.action.back", "menu.action.back.tooltip", "default"),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
}

func (svc *SlashCommandService) repositoryOnboardingRepositoryNavigation(command MenuActionCommand, accountName string) []MattermostCardAction {
	projectID := repositoryOnboardingProjectID(command)
	searchResource := menuResourceGitHubAccount
	searchID := accountName
	if projectID > 0 {
		searchResource = menuResourceProject
		searchID = strconv.FormatInt(projectID, 10)
		return []MattermostCardAction{
			svc.menuResourceDialogAction(command.View, "reposearch", menuDialogRepositorySearch, searchResource, searchID, "menu.action.repository_search", "menu.action.repository_search.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(projectID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
			svc.menuResourceAction(menuViewRepositories, "repolist", menuActionList, menuResourceRepository, "", "menu.action.repo_list", "menu.action.repo_list.tooltip", "default", nil),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		}
	}
	return []MattermostCardAction{
		svc.menuResourceDialogAction(command.View, "reposearch", menuDialogRepositorySearch, searchResource, searchID, "menu.action.repository_search", "menu.action.repository_search.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewRepositories, "repoaccounts", menuActionRepositoryOnboard, menuResourceRepository, "", "menu.action.repository_accounts", "menu.action.repository_accounts.tooltip", "default", nil),
		svc.menuResourceAction(menuViewRepositories, "repolist", menuActionList, menuResourceRepository, "", "menu.action.repo_list", "menu.action.repo_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
}

func repositoryOnboardingProjectID(command MenuActionCommand) int64 {
	if command.Resource == menuResourceProject {
		if projectID, ok := parseInt64ID(command.ID); ok {
			return projectID
		}
	}
	state, ok := parseRepositoryOnboardingResourceID(command.ID)
	if ok && state.ProjectID > 0 {
		return state.ProjectID
	}
	return 0
}

func repositoryOnboardingBranchNames(defaultBranch string, branches []providerrepo.BranchCandidate) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(branches)+1)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(defaultBranch)
	for _, branch := range branches {
		add(branch.Name)
	}
	return names
}

func repositorySearchRepositoryOptionText(candidate providerrepo.RepositoryCandidate) string {
	text := strings.TrimSpace(candidate.FullName)
	if branch := strings.TrimSpace(candidate.DefaultBranch); branch != "" {
		text += " (" + branch + ")"
	}
	return text
}

func (svc *SlashCommandService) openAIAccountListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewOpenAI)
	card.Title = svc.t("menu.entity.openai.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("openai.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewOpenAI)
		return card
	}
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		card.Text = svc.t("openai.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewOpenAI)
		return card
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	card.Text = svc.entityListText(len(accounts), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(accounts) == 0 {
		card.Text = svc.t("openai.list.empty", nil)
		card.Actions = append(card.Actions, svc.menuDialogAction(menuViewOpenAI, "dialogopenaiauth", menuDialogOpenAIAuth, "menu.action.openai_auth", "menu.action.openai_auth.tooltip", "primary"))
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewOpenAI)...)
		return card
	}
	start, end, page := entityPageBounds(len(accounts), command.Page)
	for idx, account := range accounts[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.openai.item_title", map[string]any{"Number": number, "Account": account.Name}),
			Value: svc.t("menu.entity.openai.summary", map[string]any{"Status": account.Status, "Secret": account.SecretRef}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewOpenAI, "openopenai"+strconv.Itoa(number), menuActionShow, menuResourceOpenAIAccount, account.Name, "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewOpenAI, menuResourceOpenAIAccount, "", page, len(accounts))...)
	card.Actions = append(card.Actions, svc.menuDialogAction(menuViewOpenAI, "dialogopenaiauth", menuDialogOpenAIAuth, "menu.action.openai_auth", "menu.action.openai_auth.tooltip", "primary"))
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewOpenAI)...)
	return card
}

func (svc *SlashCommandService) openAIAccountEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewOpenAI)
	card.Title = svc.t("menu.entity.openai.card_title", map[string]any{"Account": command.ID})
	account, ok := svc.openAIAccount(ctx, command.ID)
	if !ok {
		card.Text = svc.t("openai.status.account_not_found", map[string]any{"Account": command.ID})
		card.Actions = svc.entityNavigationActions(menuViewOpenAI)
		return card
	}
	card.Text = svc.t("menu.entity.openai.card_text", map[string]any{"Account": account.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.account", nil), Value: "`" + account.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.status", nil), Value: "`" + account.Status + "`", Short: true},
		{Title: svc.t("menu.entity.field.secret", nil), Value: "`" + account.SecretRef + "`", Short: false},
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewOpenAI, "openaistatus", menuActionOpenAIStatus, menuResourceOpenAIAccount, account.Name, "menu.action.openai_status", "menu.action.openai_status.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewOpenAI, "openaiauthrestart", menuActionOpenAIAuth, menuResourceOpenAIAccount, account.Name, "menu.action.openai_auth_restart", "menu.action.openai_auth_restart.tooltip", "warning", nil),
		svc.menuResourceAction(menuViewOpenAI, "openaicleanup", menuActionOpenAICleanup, menuResourceOpenAIAccount, account.Name, "menu.action.openai_cleanup", "menu.action.openai_cleanup.tooltip", "default", nil),
		svc.menuResourceAction(menuViewOpenAI, "openaideleteconfirm", menuActionConfirmDelete, menuResourceOpenAIAccount, account.Name, "menu.action.openai_delete", "menu.action.openai_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewOpenAI, "openailist", menuActionList, menuResourceOpenAIAccount, "", "menu.action.openai_list", "menu.action.openai_list.tooltip", "default", nil),
		svc.menuAction(menuViewAccounts, "menu.action.back", "menu.action.back.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) openAIAccountDeleteConfirmationCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewOpenAI)
	card.Title = svc.t("menu.confirm.openai_delete.title", nil)
	card.Text = svc.t("menu.confirm.openai_delete.text", map[string]any{"Account": command.ID})
	card.Fields = []MattermostCardField{{Title: svc.t("menu.entity.field.account", nil), Value: "`" + command.ID + "`", Short: false}}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewOpenAI, "openaidelete", menuActionDelete, menuResourceOpenAIAccount, command.ID, "menu.action.confirm_delete", "menu.action.confirm_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewOpenAI, "openaicancel", menuActionShow, menuResourceOpenAIAccount, command.ID, "menu.action.cancel", "menu.action.cancel.tooltip", "default", nil),
	}
	return card
}

func (svc *SlashCommandService) githubAccountListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewGitHub)
	card.Title = svc.t("menu.entity.github.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("github.account.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewGitHub)
		return card
	}
	accounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		card.Text = svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewGitHub)
		return card
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	card.Text = svc.entityListText(len(accounts), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(accounts) == 0 {
		card.Text = svc.t("github.account.list.empty", nil)
		card.Actions = append(card.Actions, svc.menuDialogAction(menuViewGitHub, "dialoggithubadd", menuDialogGitHubAccountAdd, "menu.action.github_account_add", "menu.action.github_account_add.tooltip", "primary"))
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewGitHub)...)
		return card
	}
	start, end, page := entityPageBounds(len(accounts), command.Page)
	for idx, account := range accounts[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.github.item_title", map[string]any{"Number": number, "Account": account.Name}),
			Value: svc.t("menu.entity.github.summary", map[string]any{"Status": account.Status, "Secret": account.SecretRef, "Username": emptyAsUnknown(account.Username), "Email": emptyAsUnknown(account.Email), "Scopes": emptyAsUnknown(account.Scopes)}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewGitHub, "opengithub"+strconv.Itoa(number), menuActionShow, menuResourceGitHubAccount, account.Name, "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewGitHub, menuResourceGitHubAccount, "", page, len(accounts))...)
	card.Actions = append(card.Actions, svc.menuDialogAction(menuViewGitHub, "dialoggithubadd", menuDialogGitHubAccountAdd, "menu.action.github_account_add", "menu.action.github_account_add.tooltip", "primary"))
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewGitHub)...)
	return card
}

func (svc *SlashCommandService) githubAccountEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewGitHub)
	card.Title = svc.t("menu.entity.github.card_title", map[string]any{"Account": command.ID})
	account, ok := svc.githubAccount(ctx, command.ID)
	if !ok {
		card.Text = svc.t("dialog.github.not_found", map[string]any{"Account": command.ID})
		card.Actions = svc.entityNavigationActions(menuViewGitHub)
		return card
	}
	card.Text = svc.t("menu.entity.github.card_text", map[string]any{"Account": account.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.account", nil), Value: "`" + account.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.status", nil), Value: "`" + account.Status + "`", Short: true},
		{Title: svc.t("menu.entity.field.secret", nil), Value: "`" + account.SecretRef + "`", Short: false},
		{Title: svc.t("menu.entity.field.username", nil), Value: "`" + emptyAsUnknown(account.Username) + "`", Short: true},
		{Title: svc.t("menu.entity.field.email", nil), Value: "`" + emptyAsUnknown(account.Email) + "`", Short: true},
		{Title: svc.t("menu.entity.field.scopes", nil), Value: "`" + emptyAsUnknown(account.Scopes) + "`", Short: false},
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceDialogAction(menuViewGitHub, "githubedit", menuDialogGitHubAccountEdit, menuResourceGitHubAccount, account.Name, "menu.action.github_account_edit", "menu.action.github_account_edit.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewGitHub, "githubdeleteconfirm", menuActionConfirmDelete, menuResourceGitHubAccount, account.Name, "menu.action.github_account_delete", "menu.action.github_account_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewGitHub, "githublist", menuActionList, menuResourceGitHubAccount, "", "menu.action.github_account_list", "menu.action.github_account_list.tooltip", "default", nil),
		svc.menuAction(menuViewAccounts, "menu.action.back", "menu.action.back.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) githubAccountDeleteConfirmationCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewGitHub)
	card.Title = svc.t("menu.confirm.github_delete.title", nil)
	card.Text = svc.t("menu.confirm.github_delete.text", map[string]any{"Account": command.ID})
	card.Fields = []MattermostCardField{{Title: svc.t("menu.entity.field.account", nil), Value: "`" + command.ID + "`", Short: false}}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewGitHub, "githubdelete", menuActionDelete, menuResourceGitHubAccount, command.ID, "menu.action.confirm_delete", "menu.action.confirm_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewGitHub, "githubcancel", menuActionShow, menuResourceGitHubAccount, command.ID, "menu.action.cancel", "menu.action.cancel.tooltip", "default", nil),
	}
	return card
}

func (svc *SlashCommandService) deleteGitHubAccountFromMenu(ctx context.Context, command MenuActionCommand) string {
	return svc.deleteGitHubAccountText(ctx, command.ID, svc.slashFromMenu(command))
}

func (svc *SlashCommandService) deleteGitHubAccountText(ctx context.Context, accountName string, command SlashCommand) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("github.account.list.storage_not_ready", nil)
	}
	account, err := svc.cfg.Store.GetGitHubAccount(ctx, accountName)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return svc.t("dialog.github.not_found", map[string]any{"Account": accountName})
		}
		return svc.t("github.account.delete_failed", map[string]any{"Error": safeError(err)})
	}
	refs, err := svc.githubAccountProfileRefs(ctx, account.Name)
	if err != nil {
		return svc.t("github.account.delete.profile_check_failed", map[string]any{"Error": safeError(err)})
	}
	if len(refs) > 0 {
		return svc.t("github.account.delete.in_use", map[string]any{"Account": account.Name, "Profiles": strings.Join(refs, ", ")})
	}
	repositoryRefs, err := svc.githubAccountRepositoryRefs(ctx, account.Name)
	if err != nil {
		return svc.t("github.account.delete.repository_check_failed", map[string]any{"Error": safeError(err)})
	}
	if len(repositoryRefs) > 0 {
		return svc.t("github.account.delete.in_use_repositories", map[string]any{"Account": account.Name, "Repositories": strings.Join(repositoryRefs, ", ")})
	}
	projectRefs, err := svc.githubAccountProjectRefs(ctx, account.Name)
	if err != nil {
		return svc.t("github.account.delete.project_check_failed", map[string]any{"Error": safeError(err)})
	}
	if len(projectRefs) > 0 {
		return svc.t("github.account.delete.in_use_projects", map[string]any{"Account": account.Name, "Projects": strings.Join(projectRefs, ", ")})
	}
	secretDeleted := false
	if account.SecretRef == githubAccountSecretName(svc.cfg.GitHubSecretName, account.Name) {
		if svc.cfg.RuntimeRunner == nil {
			return svc.t("github.account.runtime_not_configured", nil)
		}
		secret, err := svc.cfg.RuntimeRunner.DeleteGitHubTokenSecret(ctx, account.Name, account.SecretRef)
		if err != nil {
			return svc.t("github.account.delete_failed", map[string]any{"Error": safeError(err)})
		}
		secretDeleted = secret.SecretDeleted
	}
	deleted, err := svc.cfg.Store.DeleteGitHubAccount(ctx, account.Name)
	if err != nil {
		return svc.t("github.account.delete_failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordGitHubAudit(ctx, command, "github.account.deleted", deleted.Name, "github account metadata and managed token secret deleted")
	return svc.t("github.account.delete_result", map[string]any{"Account": deleted.Name, "Secret": deleted.SecretRef, "SecretDeleted": secretDeleted})
}

func (svc *SlashCommandService) profileListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProfiles)
	card.Title = svc.t("menu.entity.profiles.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("profile.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewProfiles)
		return card
	}
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		card.Text = svc.t("profile.list.read_failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProfiles)
		return card
	}
	sort.Slice(profiles, func(i int, j int) bool { return profiles[i].Name < profiles[j].Name })
	card.Text = svc.entityListText(len(profiles), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(profiles) == 0 {
		card.Text = svc.t("profile.list.empty", nil)
		card.Actions = []MattermostCardAction{
			svc.menuDialogAction(menuViewProfiles, "dialogprofileadd", menuDialogProfileUpsert, "menu.action.profile_add", "menu.action.profile_add.tooltip", "primary"),
		}
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewProfiles)...)
		return card
	}
	start, end, page := entityPageBounds(len(profiles), command.Page)
	for idx, profile := range profiles[start:end] {
		number := start + idx + 1
		enabled := svc.t("label.disabled", nil)
		if profile.Enabled {
			enabled = svc.t("label.enabled", nil)
		}
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.profile.item_title", map[string]any{"Number": number, "Profile": profile.Name}),
			Value: svc.t("menu.entity.profile.summary", map[string]any{"Role": profile.Role, "Enabled": enabled, "OpenAI": defaultString(profile.OpenAIAccountName, "primary"), "GitHub": defaultString(profile.GitHubAccountName, "primary")}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewProfiles, "openprofile"+strconv.Itoa(number), menuActionShow, menuResourceProfile, profile.Name, "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewProfiles, menuResourceProfile, "", page, len(profiles))...)
	card.Actions = append(card.Actions, svc.menuDialogAction(menuViewProfiles, "dialogprofileadd", menuDialogProfileUpsert, "menu.action.profile_add", "menu.action.profile_add.tooltip", "primary"))
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewProfiles)...)
	return card
}

func (svc *SlashCommandService) profileEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProfiles)
	card.Title = svc.t("menu.entity.profile.card_title", map[string]any{"Profile": command.ID})
	profile, ok := svc.agentProfile(ctx, command.ID)
	if !ok {
		card.Text = svc.t("dev.profile_not_ready", map[string]any{"Profile": command.ID})
		card.Actions = svc.entityNavigationActions(menuViewProfiles)
		return card
	}
	enabled := svc.t("label.disabled", nil)
	if profile.Enabled {
		enabled = svc.t("label.enabled", nil)
	}
	card.Text = svc.t("menu.entity.profile.card_text", map[string]any{"Profile": profile.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.profile", nil), Value: "`" + profile.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.role", nil), Value: "`" + profile.Role + "`", Short: true},
		{Title: svc.t("menu.entity.field.status", nil), Value: "`" + enabled + "`", Short: true},
		{Title: svc.t("menu.entity.field.openai", nil), Value: "`" + defaultString(profile.OpenAIAccountName, "primary") + "`", Short: true},
		{Title: svc.t("menu.entity.field.github", nil), Value: "`" + defaultString(profile.GitHubAccountName, "primary") + "`", Short: true},
		{Title: svc.t("menu.entity.field.kubernetes_access", nil), Value: "`" + defaultString(profile.KubernetesAccess, "read-only") + "`", Short: true},
		{Title: svc.t("menu.entity.field.sandbox", nil), Value: "`" + defaultString(profile.SandboxMode, "danger-full-access") + "`", Short: true},
		{Title: svc.t("menu.entity.field.description", nil), Value: emptyAsUnknown(profile.Description), Short: false},
	}
	statusAction := svc.menuResourceAction(menuViewProfiles, "profiledisable", menuActionProfileDisable, menuResourceProfile, profile.Name, "menu.action.profile_disable", "menu.action.profile_disable.tooltip", "warning", nil)
	if !profile.Enabled {
		statusAction = svc.menuResourceAction(menuViewProfiles, "profileenable", menuActionProfileEnable, menuResourceProfile, profile.Name, "menu.action.profile_enable", "menu.action.profile_enable.tooltip", "primary", nil)
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceDialogAction(menuViewProfiles, "profileedit", menuDialogProfileUpsert, menuResourceProfile, profile.Name, "menu.action.profile_edit", "menu.action.profile_edit.tooltip", "primary", nil),
		statusAction,
		svc.menuResourceAction(menuViewPrompts, "profileprompts", menuActionList, menuResourcePromptTemplate, profile.Name, "menu.action.prompts", "menu.action.prompts.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewProfiles, "profilelist", menuActionList, menuResourceProfile, "", "menu.action.profile_list", "menu.action.profile_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) promptTemplateListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewPrompts)
	card.Title = svc.t("menu.entity.prompts.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("prompt.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewPrompts)
		return card
	}
	profileName := ""
	if command.Resource == menuResourcePromptTemplate && validPromptTemplateID(command.ID) {
		profileName = command.ID
	}
	templates, err := svc.cfg.Store.ListAgentPromptTemplates(ctx, profileName)
	if err != nil {
		card.Text = svc.t("prompt.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPrompts)
		return card
	}
	sort.Slice(templates, func(i int, j int) bool {
		return promptTemplateResourceID(templates[i].ProfileName, templates[i].TemplateKey) < promptTemplateResourceID(templates[j].ProfileName, templates[j].TemplateKey)
	})
	card.Text = svc.entityListText(len(templates), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(templates) == 0 {
		card.Text = svc.t("prompt.list.empty", nil)
		card.Actions = svc.entityNavigationActions(menuViewPrompts)
		return card
	}
	start, end, page := entityPageBounds(len(templates), command.Page)
	for idx, item := range templates[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.prompt.item_title", map[string]any{"Number": number, "Profile": item.ProfileName, "Template": item.TemplateKey}),
			Value: svc.t("menu.entity.prompt.summary", map[string]any{"Bytes": len(item.Body)}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewPrompts, "openprompt"+strconv.Itoa(number), menuActionShow, menuResourcePromptTemplate, promptTemplateResourceID(item.ProfileName, item.TemplateKey), "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewPrompts, menuResourcePromptTemplate, command.ID, page, len(templates))...)
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewPrompts)...)
	return card
}

func (svc *SlashCommandService) promptTemplateEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewPrompts)
	profileName, templateKey, ok := parsePromptTemplateResourceID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewPrompts)
		return card
	}
	card.Title = svc.t("menu.entity.prompt.card_title", map[string]any{"Profile": profileName, "Template": templateKey})
	item, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, profileName, templateKey)
	if err != nil {
		card.Text = svc.t("prompt.show.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPrompts)
		return card
	}
	card.Text = svc.t("menu.entity.prompt.card_text", map[string]any{"Profile": item.ProfileName, "Template": item.TemplateKey})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.profile", nil), Value: "`" + item.ProfileName + "`", Short: true},
		{Title: svc.t("menu.entity.field.template", nil), Value: "`" + item.TemplateKey + "`", Short: true},
		{Title: svc.t("menu.entity.field.bytes", nil), Value: "`" + strconv.Itoa(len(item.Body)) + "`", Short: true},
	}
	resourceID := promptTemplateResourceID(item.ProfileName, item.TemplateKey)
	card.Actions = []MattermostCardAction{
		svc.menuResourceDialogAction(menuViewPrompts, "promptedit", menuDialogPromptEdit, menuResourcePromptTemplate, resourceID, "menu.action.prompt_edit", "menu.action.prompt_edit.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewPrompts, "prompthelp", menuActionPromptHelp, menuResourcePromptTemplate, resourceID, "menu.action.prompt_help", "menu.action.prompt_help.tooltip", "default", nil),
		svc.menuResourceAction(menuViewPrompts, "promptrender", menuActionPromptRender, menuResourcePromptTemplate, resourceID, "menu.action.prompt_render", "menu.action.prompt_render.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewPrompts, "promptlist", menuActionList, menuResourcePromptTemplate, "", "menu.action.prompt_list", "menu.action.prompt_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) flowListCard(ctx context.Context, command MenuActionCommand, pendingOnly bool) *MattermostCard {
	card := svc.menuCard(ctx, menuViewPending)
	card.Title = svc.t("menu.entity.flows.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("flow.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	flows, err := svc.cfg.Store.ListAgentFlows(ctx, "", 100)
	if err != nil {
		card.Text = svc.t("flow.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	items := make([]entity.AgentFlow, 0, len(flows))
	for _, flow := range flows {
		if pendingOnly || command.View == menuViewPending {
			if !flowNeedsOwnerAttention(flow) {
				continue
			}
		}
		items = append(items, flow)
	}
	card.Text = svc.entityListText(len(items), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(items) == 0 {
		card.Text = svc.t("flow.list.empty", nil)
		card.Actions = []MattermostCardAction{
			svc.menuDialogAction(menuViewStartFlow, "dialogflowstart", menuDialogFlowStart, "menu.action.flow_start", "menu.action.flow_start.tooltip", "primary"),
		}
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewPending)...)
		return card
	}
	start, end, page := entityPageBounds(len(items), command.Page)
	for idx, flow := range items[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.flow.item_title", map[string]any{"Number": number, "Flow": flow.FlowID}),
			Value: svc.t("menu.entity.flow.summary", map[string]any{
				"Status":     flow.Status,
				"Repository": flow.FullName(),
				"PR":         flow.PRNumber,
				"Developer":  defaultString(flow.DeveloperProfileName, "developer"),
				"Reviewer":   defaultString(flow.ReviewerProfileName, "reviewer"),
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewPending, "openflow"+strconv.Itoa(number), menuActionShow, menuResourceFlow, flow.FlowID, "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewPending, menuResourceFlow, "", page, len(items))...)
	card.Actions = append(card.Actions, svc.menuDialogAction(menuViewStartFlow, "dialogflowstart", menuDialogFlowStart, "menu.action.flow_start", "menu.action.flow_start.tooltip", "primary"))
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewPending)...)
	return card
}

func (svc *SlashCommandService) flowEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewPending)
	card.Title = svc.t("menu.entity.flow.card_title", map[string]any{"Flow": command.ID})
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("flow.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, command.ID)
	if err != nil {
		card.Text = svc.t("flow.status.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	card.Text = svc.flowCardText(flow)
	card.Color = flowCardColor(flow.Status)
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.status", nil), Value: "`" + flow.Status + "`", Short: true},
		{Title: svc.t("menu.entity.field.repository", nil), Value: "`" + flow.FullName() + "`", Short: true},
		{Title: svc.t("menu.entity.field.branch", nil), Value: "`" + flow.HeadBranch + "`", Short: true},
		{Title: svc.t("menu.entity.field.developer_profile", nil), Value: "`" + defaultString(flow.DeveloperProfileName, "developer") + "`", Short: true},
		{Title: svc.t("menu.entity.field.reviewer_profile", nil), Value: "`" + defaultString(flow.ReviewerProfileName, "reviewer") + "`", Short: true},
		{Title: svc.t("menu.entity.field.pr", nil), Value: flowPRValue(flow), Short: true},
	}
	actions := []MattermostCardAction{
		svc.menuResourceAction(menuViewPending, "flowadvance", menuActionFlowAdvance, menuResourceFlow, flow.FlowID, "menu.action.flow_advance", "menu.action.flow_advance.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewPending, "flowcard", menuActionFlowCard, menuResourceFlow, flow.FlowID, "menu.action.flow_card", "menu.action.flow_card.tooltip", "default", nil),
	}
	if flowCanOwnerDecide(flow) {
		actions = append(actions,
			svc.menuResourceAction(menuViewPending, "flowapprove", menuActionFlowApprove, menuResourceFlow, flow.FlowID, "flow.card.action.approve", "flow.card.action.approve.tooltip", "success", nil),
			svc.menuResourceAction(menuViewPending, "flowreject", menuActionFlowReject, menuResourceFlow, flow.FlowID, "flow.card.action.reject", "flow.card.action.reject.tooltip", "danger", nil),
		)
	}
	if flowCanOwnerRerun(flow) {
		actions = append(actions, svc.menuResourceAction(menuViewPending, "flowrerun", menuActionFlowRerun, menuResourceFlow, flow.FlowID, "flow.card.action.rerun", "flow.card.action.rerun.tooltip", "primary", nil))
	}
	if flow.Status == flowStatusHeld {
		actions = append(actions, svc.menuResourceAction(menuViewPending, "flowresume", menuActionFlowResume, menuResourceFlow, flow.FlowID, "menu.action.flow_resume", "menu.action.flow_resume.tooltip", "primary", nil))
	} else if flowCanHold(flow) {
		actions = append(actions, svc.menuResourceAction(menuViewPending, "flowhold", menuActionFlowHold, menuResourceFlow, flow.FlowID, "menu.action.flow_hold", "menu.action.flow_hold.tooltip", "warning", nil))
	}
	if !flowOwnerTerminal(flow.Status) {
		actions = append(actions, svc.menuResourceAction(menuViewPending, "flowstop", menuActionFlowStop, menuResourceFlow, flow.FlowID, "flow.card.action.stop", "flow.card.action.stop.tooltip", "warning", nil))
	}
	actions = append(actions,
		svc.menuResourceAction(menuViewPending, "flowcleanup", menuActionFlowCleanup, menuResourceFlow, flow.FlowID, "menu.action.flow_cleanup", "menu.action.flow_cleanup.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewPending, "flowlist", menuActionList, menuResourceFlow, "", "menu.action.pending_list", "menu.action.pending_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	)
	card.Actions = actions
	return card
}

func (svc *SlashCommandService) runtimeRunListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRuntime)
	card.Title = svc.t("menu.entity.runs.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("runtime.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewRuntime)
		return card
	}
	runs, err := svc.cfg.Store.ListAgentRuns(ctx, 100)
	if err != nil {
		card.Text = svc.t("runtime.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewRuntime)
		return card
	}
	card.Text = svc.entityListText(len(runs), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(runs) == 0 {
		card.Text = svc.t("runtime.list.empty", nil)
		card.Actions = svc.menuActions(menuViewRuntime)
		return card
	}
	start, end, page := entityPageBounds(len(runs), command.Page)
	for idx, run := range runs[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.run.item_title", map[string]any{"Number": number, "Run": run.RunID}),
			Value: svc.t("menu.entity.run.summary", map[string]any{
				"Status":     run.Status,
				"Profile":    run.ProfileName,
				"Repository": run.FullName(),
				"Flow":       emptyAsUnknown(run.FlowID),
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewRuntime, "openrun"+strconv.Itoa(number), menuActionShow, menuResourceRun, run.RunID, "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewRuntime, menuResourceRun, "", page, len(runs))...)
	card.Actions = append(card.Actions, svc.menuActions(menuViewRuntime)...)
	return card
}

func (svc *SlashCommandService) runtimeRunEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRuntime)
	card.Title = svc.t("menu.entity.run.card_title", map[string]any{"Run": command.ID})
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("runtime.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewRuntime)
		return card
	}
	run, err := svc.cfg.Store.GetAgentRun(ctx, command.ID)
	if err != nil {
		card.Text = svc.t("runtime.status.not_found", map[string]any{"RunID": command.ID})
		card.Actions = svc.entityNavigationActions(menuViewRuntime)
		return card
	}
	status := run.Status
	text := svc.t("menu.entity.run.card_text", map[string]any{"Run": run.RunID})
	if svc.cfg.RuntimeRunner != nil {
		if runtimeStatus, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, run.RunID); err == nil {
			status = runtimeDerivedStatus(run.Status, runtimeStatus)
			text = svc.runtimeStatusBrief(runtimeStatus)
		}
	}
	card.Text = text
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.status", nil), Value: "`" + status + "`", Short: true},
		{Title: svc.t("menu.entity.field.profile", nil), Value: "`" + emptyAsUnknown(run.ProfileName) + "`", Short: true},
		{Title: svc.t("menu.entity.field.repository", nil), Value: "`" + run.FullName() + "`", Short: true},
		{Title: svc.t("menu.entity.field.flow", nil), Value: "`" + emptyAsUnknown(run.FlowID) + "`", Short: true},
		{Title: svc.t("menu.entity.field.job", nil), Value: "`" + emptyAsUnknown(run.JobName) + "`", Short: true},
		{Title: svc.t("menu.entity.field.pvc", nil), Value: "`" + emptyAsUnknown(run.PVCName) + "`", Short: true},
	}
	actions := []MattermostCardAction{
		svc.menuResourceAction(menuViewRuntime, "runcleanup", menuActionRuntimeCleanup, menuResourceRun, run.RunID, "menu.action.runtime_cleanup", "menu.action.runtime_cleanup.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewRuntime, "runlist", menuActionList, menuResourceRun, "", "menu.action.runtime_runs", "menu.action.runtime_runs.tooltip", "default", nil),
	}
	if run.FlowID != "" {
		actions = append(actions, svc.menuResourceAction(menuViewPending, "openflow", menuActionShow, menuResourceFlow, run.FlowID, "menu.action.flow_open", "menu.action.flow_open.tooltip", "default", nil))
	}
	actions = append(actions, svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"))
	card.Actions = actions
	return card
}

func (svc *SlashCommandService) entityListText(total int, page int) string {
	if total == 0 {
		return svc.t("menu.entity.list.empty", nil)
	}
	_, _, normalizedPage := entityPageBounds(total, page)
	pages := (total + entityListPageSize - 1) / entityListPageSize
	return svc.t("menu.entity.list.text", map[string]any{"Total": total, "Page": normalizedPage + 1, "Pages": pages})
}

func (svc *SlashCommandService) pageActions(view string, resource string, resourceID string, page int, total int) []MattermostCardAction {
	var actions []MattermostCardAction
	pages := (total + entityListPageSize - 1) / entityListPageSize
	if page > 0 {
		action := svc.menuResourceAction(view, "pageprev", menuActionList, resource, resourceID, "menu.action.prev_page", "menu.action.prev_page.tooltip", "default", nil)
		action.Context["page"] = page - 1
		actions = append(actions, action)
	}
	if page+1 < pages {
		action := svc.menuResourceAction(view, "pagenext", menuActionList, resource, resourceID, "menu.action.next_page", "menu.action.next_page.tooltip", "default", nil)
		action.Context["page"] = page + 1
		actions = append(actions, action)
	}
	return actions
}

func (svc *SlashCommandService) entityNavigationActions(view string) []MattermostCardAction {
	if view == menuViewMain {
		return nil
	}
	return []MattermostCardAction{
		svc.menuAction(view, "menu.action.back", "menu.action.back.tooltip", "default"),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
}

func (svc *SlashCommandService) menuResourceAction(view string, actionID string, action string, resource string, resourceID string, nameID string, tooltipID string, style string, data map[string]any) MattermostCardAction {
	item := svc.menuAction(view, nameID, tooltipID, style)
	item.ID = actionID
	item.Name = svc.t(nameID, data)
	item.Tooltip = svc.t(tooltipID, data)
	item.Context["action"] = action
	item.Context["resource_type"] = resource
	if strings.TrimSpace(resourceID) != "" {
		item.Context["resource_id"] = resourceID
	}
	return item
}

func (svc *SlashCommandService) menuResourceDialogAction(view string, actionID string, dialog string, resource string, resourceID string, nameID string, tooltipID string, style string, data map[string]any) MattermostCardAction {
	item := svc.menuAction(view, nameID, tooltipID, style)
	item.ID = actionID
	item.Name = svc.t(nameID, data)
	item.Tooltip = svc.t(tooltipID, data)
	item.Context["dialog"] = dialog
	item.Context["resource_type"] = resource
	if strings.TrimSpace(resourceID) != "" {
		item.Context["resource_id"] = resourceID
	}
	return item
}

func (svc *SlashCommandService) slashFromMenu(command MenuActionCommand) SlashCommand {
	return SlashCommand{
		UserID:    command.UserID,
		UserName:  command.UserName,
		ChannelID: command.ChannelID,
	}
}

func (svc *SlashCommandService) menuFlowActionResult(ctx context.Context, command MenuActionCommand, action string) MenuActionResult {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.menuActionTextResult(ctx, command, svc.t("flow.storage_not_ready", nil), false)
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, command.ID)
	if err != nil {
		return svc.menuActionTextResult(ctx, command, svc.t("flow.status.failed", map[string]any{"Error": safeError(err)}), false)
	}
	result := svc.HandleFlowAction(ctx, FlowActionCommand{
		FlowID:    flow.FlowID,
		Action:    action,
		Token:     flow.ActionToken,
		UserID:    command.UserID,
		UserName:  command.UserName,
		ChannelID: command.ChannelID,
	})
	return MenuActionResult{
		StatusCode:    result.StatusCode,
		EphemeralText: result.EphemeralText,
		Card:          svc.flowEntityCard(ctx, command),
	}
}

func (svc *SlashCommandService) updateProfileEnabledCard(ctx context.Context, command MenuActionCommand, enabled bool) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProfiles)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("profile.list.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewProfiles)
		return card
	}
	profile, err := svc.cfg.Store.GetAgentProfile(ctx, command.ID)
	if err != nil {
		card.Text = svc.t("dev.profile_not_ready", map[string]any{"Profile": command.ID})
		card.Actions = svc.entityNavigationActions(menuViewProfiles)
		return card
	}
	profile.Enabled = enabled
	_, _, err = svc.cfg.Store.UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
		Name:              profile.Name,
		Role:              profile.Role,
		Description:       profile.Description,
		Enabled:           profile.Enabled,
		OpenAIAccountName: defaultString(profile.OpenAIAccountName, "primary"),
		GitHubAccountName: defaultString(profile.GitHubAccountName, "primary"),
		KubernetesAccess:  defaultString(profile.KubernetesAccess, "read-only"),
		SandboxMode:       defaultString(profile.SandboxMode, "danger-full-access"),
		ConfigOverlay:     profile.ConfigOverlay,
	})
	if err != nil {
		card.Text = svc.t("profile.save.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProfiles)
		return card
	}
	eventType := "agent_profile.disabled"
	if enabled {
		eventType = "agent_profile.enabled"
	}
	svc.recordProfileAudit(ctx, svc.slashFromMenu(command), eventType, profile.Name, "agent profile enabled flag changed from Mattermost menu")
	return svc.profileEntityCard(ctx, command)
}

func (svc *SlashCommandService) advanceFlowCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewPending)
	if svc.cfg.RuntimeRunner == nil {
		card.Text = svc.t("runtime.not_configured", nil)
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("flow.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, command.ID)
	if err != nil {
		card.Text = svc.t("flow.status.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	flow, events := svc.advanceFlow(ctx, flow)
	_ = svc.refreshFlowCard(ctx, flow)
	card = svc.flowEntityCard(ctx, command)
	if len(events) > 0 {
		card.Text = svc.flowStatusText(flow, events)
	}
	return card
}

func (svc *SlashCommandService) updateFlowHoldCard(ctx context.Context, command MenuActionCommand, hold bool) *MattermostCard {
	card := svc.menuCard(ctx, menuViewPending)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("flow.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	flow, err := svc.cfg.Store.GetAgentFlow(ctx, command.ID)
	if err != nil {
		card.Text = svc.t("flow.status.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	status := flowStatusWaitingOwner
	summary := "owner resumed flow from Mattermost menu"
	eventType := "flow.resumed"
	if hold {
		if !flowCanHold(flow) {
			card.Text = svc.t("flow.action.hold_not_ready", nil)
			card.Actions = svc.entityNavigationActions(menuViewPending)
			return card
		}
		status = flowStatusHeld
		summary = "owner held flow from Mattermost menu"
		eventType = "flow.held"
	}
	updated, err := svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{
		FlowID:  flow.FlowID,
		Status:  status,
		Summary: summary,
	})
	if err != nil {
		card.Text = svc.t("flow.action.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewPending)
		return card
	}
	svc.recordFlowAudit(ctx, svc.slashFromMenu(command), eventType, updated.FlowID, summary)
	_ = svc.refreshFlowCard(ctx, updated)
	return svc.flowEntityCard(ctx, command)
}

func (svc *SlashCommandService) HandleDialogSubmission(ctx context.Context, command DialogSubmissionCommand) DialogSubmissionResult {
	if command.Cancelled {
		return DialogSubmissionResult{StatusCode: 200}
	}
	state, err := decodeDialogState(command.State)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 400, Error: svc.t("dialog.state_invalid", nil)}
	}
	switch strings.TrimSpace(command.CallbackID) {
	case dialogCallbackProjectUpsert:
		return svc.handleProjectDialogUpsert(ctx, command, state)
	case dialogCallbackProjectRepositoryBind:
		return svc.handleProjectRepositoryBindDialog(ctx, command, state)
	case dialogCallbackAgentRoleUpsert:
		return svc.handleAgentRoleDialogUpsert(ctx, command, state)
	case dialogCallbackChatCreate:
		return svc.handleChatCreateDialog(ctx, command, state)
	case dialogCallbackRepositoryAdd:
		return svc.handleRepositoryDialogUpsert(ctx, command, state, false)
	case dialogCallbackRepositoryEdit:
		return svc.handleRepositoryDialogUpsert(ctx, command, state, true)
	case dialogCallbackRepositoryDelete:
		return svc.handleRepositoryDialogDelete(ctx, command, state)
	case dialogCallbackRepositorySearch:
		return svc.handleRepositorySearchDialog(ctx, command, state)
	case dialogCallbackRepositorySearchPick:
		return svc.handleRepositorySearchPickDialog(ctx, command, state)
	case dialogCallbackRepositorySearchBranch:
		return svc.handleRepositorySearchBranchDialog(ctx, command, state)
	case dialogCallbackOpenAIAuth:
		return svc.handleOpenAIAccountDialog(ctx, command, state, openAIDialogActionAuth)
	case dialogCallbackOpenAIStatus:
		return svc.handleOpenAIAccountDialog(ctx, command, state, openAIDialogActionStatus)
	case dialogCallbackOpenAICleanup:
		return svc.handleOpenAIAccountDialog(ctx, command, state, openAIDialogActionCleanup)
	case dialogCallbackOpenAIDelete:
		return svc.handleOpenAIAccountDialog(ctx, command, state, openAIDialogActionDelete)
	case dialogCallbackGitHubAccountAdd:
		return svc.handleGitHubAccountDialogUpsert(ctx, command, state, false)
	case dialogCallbackGitHubAccountEdit:
		return svc.handleGitHubAccountDialogUpsert(ctx, command, state, true)
	case dialogCallbackGitHubAccountDelete:
		return svc.handleGitHubAccountDialogDelete(ctx, command, state)
	case dialogCallbackProfileUpsert:
		return svc.handleProfileDialogUpsert(ctx, command, state)
	case dialogCallbackPromptEdit:
		return svc.handlePromptEditDialog(ctx, command, state)
	case dialogCallbackFlowStart:
		return svc.handleFlowStartDialog(ctx, command, state)
	case dialogCallbackRuntimePruneApply:
		return svc.handleRuntimePruneDialog(ctx, command, state)
	default:
		return DialogSubmissionResult{StatusCode: 400, Error: svc.t("dialog.unknown", nil)}
	}
}

func (svc *SlashCommandService) menuDialog(ctx context.Context, command MenuActionCommand, dialogID string) (*MattermostDialog, string) {
	if strings.TrimSpace(svc.cfg.DialogSubmitURL) == "" {
		return nil, svc.t("dialog.open.not_configured", nil)
	}
	switch dialogID {
	case menuDialogProjectUpsert:
		return svc.projectDialog(ctx, command)
	case menuDialogProjectRepositoryBind:
		return svc.projectRepositoryBindDialog(ctx, command)
	case menuDialogAgentRoleUpsert:
		return svc.agentRoleDialog(ctx, command)
	case menuDialogChatCreate:
		return svc.chatDialog(ctx, command)
	case menuDialogRepositoryAdd:
		return svc.repositoryDialog(command, dialogCallbackRepositoryAdd, "dialog.repo.add.title", "dialog.repo.add.intro", "dialog.repo.add.submit", false), ""
	case menuDialogRepositoryEdit:
		return svc.repositoryDialog(command, dialogCallbackRepositoryEdit, "dialog.repo.edit.title", "dialog.repo.edit.intro", "dialog.repo.edit.submit", false), ""
	case menuDialogRepositoryDelete:
		return svc.repositoryDialog(command, dialogCallbackRepositoryDelete, "dialog.repo.delete.title", "dialog.repo.delete.intro", "dialog.repo.delete.submit", true), ""
	case menuDialogRepositorySearch:
		return svc.repositorySearchDialog(command), ""
	case menuDialogOpenAIAuth:
		return svc.openAIAccountDialog(command, dialogCallbackOpenAIAuth, "dialog.openai.auth.title", "dialog.openai.auth.intro", "dialog.openai.auth.submit"), ""
	case menuDialogOpenAIStatus:
		return svc.openAIAccountDialog(command, dialogCallbackOpenAIStatus, "dialog.openai.status.title", "dialog.openai.status.intro", "dialog.openai.status.submit"), ""
	case menuDialogOpenAICleanup:
		return svc.openAIAccountDialog(command, dialogCallbackOpenAICleanup, "dialog.openai.cleanup.title", "dialog.openai.cleanup.intro", "dialog.openai.cleanup.submit"), ""
	case menuDialogOpenAIDelete:
		return svc.openAIAccountDeleteDialog(command), ""
	case menuDialogGitHubAccountAdd:
		return svc.githubAccountDialog(command, dialogCallbackGitHubAccountAdd, "dialog.github.add.title", "dialog.github.add.intro", "dialog.github.add.submit", false), ""
	case menuDialogGitHubAccountEdit:
		return svc.githubAccountDialog(command, dialogCallbackGitHubAccountEdit, "dialog.github.edit.title", "dialog.github.edit.intro", "dialog.github.edit.submit", false), ""
	case menuDialogGitHubAccountDelete:
		return svc.githubAccountDialog(command, dialogCallbackGitHubAccountDelete, "dialog.github.delete.title", "dialog.github.delete.intro", "dialog.github.delete.submit", true), ""
	case menuDialogProfileUpsert:
		return svc.profileDialog(ctx, command)
	case menuDialogPromptEdit:
		return svc.promptEditDialog(ctx, command)
	case menuDialogFlowStart:
		return svc.flowStartDialog(ctx, command)
	case menuDialogRuntimePruneApply:
		return svc.runtimePruneApplyDialog(command), ""
	default:
		return nil, svc.t("dialog.unknown", nil)
	}
}

func (svc *SlashCommandService) repositoryDialog(command MenuActionCommand, callbackID string, titleID string, introID string, submitID string, deleteMode bool) *MattermostDialog {
	elements := []MattermostDialogElement{
		{
			DisplayName: svc.t("dialog.repo.field.provider", nil),
			Name:        dialogFieldProvider,
			Type:        "select",
			Default:     "github",
			Options: []MattermostDialogOption{
				{Text: "GitHub", Value: "github"},
			},
		},
		{
			DisplayName: svc.t("dialog.repo.field.repository", nil),
			Name:        dialogFieldRepository,
			Type:        "text",
			Placeholder: "codex-k8s/matter-codex",
			HelpText:    svc.t("dialog.repo.field.repository.help", nil),
			MinLength:   3,
			MaxLength:   120,
		},
	}
	if deleteMode {
		elements = append(elements, MattermostDialogElement{
			DisplayName: svc.t("dialog.repo.field.confirm", nil),
			Name:        dialogFieldConfirm,
			Type:        "text",
			Placeholder: "delete",
			HelpText:    svc.t("dialog.repo.field.confirm.help", nil),
			MinLength:   6,
			MaxLength:   6,
		})
	} else {
		elements = append(elements, MattermostDialogElement{
			DisplayName: svc.t("dialog.repo.field.branch", nil),
			Name:        dialogFieldDefaultBranch,
			Type:        "text",
			Default:     "main",
			Placeholder: "main",
			HelpText:    svc.t("dialog.repo.field.branch.help", nil),
			MinLength:   1,
			MaxLength:   120,
		})
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       callbackID,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements:         elements,
		SubmitLabel:      svc.t(submitID, nil),
		State:            encodeDialogState(command),
	}
}

func (svc *SlashCommandService) repositorySearchDialog(command MenuActionCommand) *MattermostDialog {
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackRepositorySearch,
		Title:            svc.t("dialog.repo.search.title", nil),
		IntroductionText: svc.t("dialog.repo.search.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.repo.field.search", nil),
				Name:        dialogFieldSearch,
				Type:        "text",
				Placeholder: "matter-codex",
				HelpText:    svc.t("dialog.repo.field.search.help", nil),
				MinLength:   2,
				MaxLength:   120,
			},
		},
		SubmitLabel: svc.t("dialog.repo.search.submit", nil),
		State:       encodeDialogState(command),
	}
}

func (svc *SlashCommandService) openAIAccountDialog(command MenuActionCommand, callbackID string, titleID string, introID string, submitID string) *MattermostDialog {
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       callbackID,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.account.field.name", nil),
				Name:        dialogFieldAccount,
				Type:        "text",
				Default:     "primary",
				Placeholder: "primary",
				HelpText:    svc.t("dialog.account.field.name.help", nil),
				MinLength:   1,
				MaxLength:   48,
			},
		},
		SubmitLabel: svc.t(submitID, nil),
		State:       encodeDialogState(command),
	}
}

func (svc *SlashCommandService) openAIAccountDeleteDialog(command MenuActionCommand) *MattermostDialog {
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackOpenAIDelete,
		Title:            svc.t("dialog.openai.delete.title", nil),
		IntroductionText: svc.t("dialog.openai.delete.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.account.field.name", nil),
				Name:        dialogFieldAccount,
				Type:        "text",
				Placeholder: "reviewer-test",
				HelpText:    svc.t("dialog.account.field.name.help", nil),
				MinLength:   1,
				MaxLength:   48,
			},
			{
				DisplayName: svc.t("dialog.repo.field.confirm", nil),
				Name:        dialogFieldConfirm,
				Type:        "text",
				Placeholder: "delete",
				HelpText:    svc.t("dialog.repo.field.confirm.help", nil),
				MinLength:   6,
				MaxLength:   6,
			},
		},
		SubmitLabel: svc.t("dialog.openai.delete.submit", nil),
		State:       encodeDialogState(command),
	}
}

func (svc *SlashCommandService) githubAccountDialog(command MenuActionCommand, callbackID string, titleID string, introID string, submitID string, deleteMode bool) *MattermostDialog {
	accountDefault := defaultString(command.ID, "agent")
	elements := []MattermostDialogElement{
		{
			DisplayName: svc.t("dialog.account.field.name", nil),
			Name:        dialogFieldAccount,
			Type:        "text",
			Default:     accountDefault,
			Placeholder: "agent",
			HelpText:    svc.t("dialog.account.field.name.help", nil),
			MinLength:   1,
			MaxLength:   48,
		},
	}
	if deleteMode {
		elements = append(elements, MattermostDialogElement{
			DisplayName: svc.t("dialog.repo.field.confirm", nil),
			Name:        dialogFieldConfirm,
			Type:        "text",
			Placeholder: "delete",
			HelpText:    svc.t("dialog.repo.field.confirm.help", nil),
			MinLength:   6,
			MaxLength:   6,
		})
	} else {
		elements = append(elements, MattermostDialogElement{
			DisplayName: svc.t("dialog.github.field.token", nil),
			Name:        dialogFieldToken,
			Type:        "text",
			SubType:     "password",
			Placeholder: "github_pat_...",
			HelpText:    svc.t("dialog.github.field.token.help", nil),
			MinLength:   8,
			MaxLength:   4096,
		})
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       callbackID,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements:         elements,
		SubmitLabel:      svc.t(submitID, nil),
		State:            encodeDialogState(command),
	}
}

func (svc *SlashCommandService) profileDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("profile.list.storage_not_ready", nil)
	}
	profile := entity.AgentProfile{
		Name:              "",
		Role:              "developer",
		Enabled:           true,
		OpenAIAccountName: "primary",
		GitHubAccountName: "agent",
		KubernetesAccess:  "read-only",
		SandboxMode:       "danger-full-access",
	}
	editMode := strings.TrimSpace(command.ID) != ""
	if editMode {
		current, err := svc.cfg.Store.GetAgentProfile(ctx, command.ID)
		if err != nil {
			return nil, svc.t("dev.profile_not_ready", map[string]any{"Profile": command.ID})
		}
		profile = current
	}
	openAIOptions, errText := svc.openAIAccountOptions(ctx, profile.OpenAIAccountName)
	if errText != "" {
		return nil, errText
	}
	githubOptions, errText := svc.githubAccountOptions(ctx, profile.GitHubAccountName)
	if errText != "" {
		return nil, errText
	}
	titleID := "dialog.profile.add.title"
	introID := "dialog.profile.add.intro"
	submitID := "dialog.profile.add.submit"
	if editMode {
		titleID = "dialog.profile.edit.title"
		introID = "dialog.profile.edit.intro"
		submitID = "dialog.profile.edit.submit"
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackProfileUpsert,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.profile.field.name", nil),
				Name:        dialogFieldProfile,
				Type:        "text",
				Default:     profile.Name,
				Placeholder: "developer",
				HelpText:    svc.t("dialog.profile.field.name.help", nil),
				MinLength:   1,
				MaxLength:   48,
			},
			{
				DisplayName: svc.t("dialog.profile.field.role", nil),
				Name:        dialogFieldRole,
				Type:        "select",
				Default:     defaultString(profile.Role, "developer"),
				Options:     profileRoleOptions(),
			},
			{
				DisplayName: svc.t("dialog.profile.field.openai", nil),
				Name:        dialogFieldOpenAIAccount,
				Type:        "select",
				Default:     defaultString(profile.OpenAIAccountName, "primary"),
				Options:     openAIOptions,
			},
			{
				DisplayName: svc.t("dialog.profile.field.github", nil),
				Name:        dialogFieldGitHubAccount,
				Type:        "select",
				Default:     defaultString(profile.GitHubAccountName, "agent"),
				Options:     githubOptions,
			},
			{
				DisplayName: svc.t("dialog.profile.field.kubernetes", nil),
				Name:        dialogFieldKubernetesAccess,
				Type:        "select",
				Default:     defaultString(profile.KubernetesAccess, "read-only"),
				Options: []MattermostDialogOption{
					{Text: svc.t("dialog.profile.kubernetes.read_only", nil), Value: "read-only"},
					{Text: svc.t("dialog.profile.kubernetes.cluster_admin", nil), Value: "cluster-admin"},
				},
			},
			{
				DisplayName: svc.t("dialog.profile.field.sandbox", nil),
				Name:        dialogFieldSandboxMode,
				Type:        "select",
				Default:     defaultString(profile.SandboxMode, "danger-full-access"),
				Options: []MattermostDialogOption{
					{Text: "danger-full-access", Value: "danger-full-access"},
					{Text: "workspace-write", Value: "workspace-write"},
					{Text: "read-only", Value: "read-only"},
				},
			},
			{
				DisplayName: svc.t("dialog.profile.field.description", nil),
				Name:        dialogFieldDescription,
				Type:        "textarea",
				Default:     profile.Description,
				HelpText:    svc.t("dialog.profile.field.description.help", nil),
				Optional:    true,
				MaxLength:   1000,
			},
			{
				DisplayName: svc.t("dialog.profile.field.config", nil),
				Name:        dialogFieldConfigOverlay,
				Type:        "textarea",
				Default:     profile.ConfigOverlay,
				Placeholder: "sandbox_mode = \"danger-full-access\"",
				HelpText:    svc.t("dialog.profile.field.config.help", nil),
				Optional:    true,
				MaxLength:   4000,
			},
		},
		SubmitLabel: svc.t(submitID, nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) promptEditDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	profileName, templateKey, ok := parsePromptTemplateResourceID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("menu.entity.invalid", nil)
	}
	item, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, profileName, templateKey)
	if err != nil {
		return nil, svc.t("prompt.show.failed", map[string]any{"Error": safeError(err)})
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackPromptEdit,
		Title:            svc.t("dialog.prompt.edit.title", nil),
		IntroductionText: svc.t("dialog.prompt.edit.intro", map[string]any{"Profile": profileName, "Template": templateKey, "Reference": svc.t("prompt.help.reference", promptTemplateReferenceData())}),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.prompt.field.body", nil),
				Name:        dialogFieldTemplateBody,
				Type:        "textarea",
				Default:     item.Body,
				HelpText:    svc.t("dialog.prompt.field.body.help", nil),
				MinLength:   1,
				MaxLength:   12000,
			},
		},
		SubmitLabel: svc.t("dialog.prompt.edit.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) flowStartDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("flow.storage_not_ready", nil)
	}
	repoOptions, errText := svc.repositoryOptions(ctx)
	if errText != "" {
		return nil, errText
	}
	profileOptions, errText := svc.enabledProfileOptions(ctx)
	if errText != "" {
		return nil, errText
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackFlowStart,
		Title:            svc.t("dialog.flow.start.title", nil),
		IntroductionText: svc.t("dialog.flow.start.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.flow.field.repository", nil),
				Name:        dialogFieldFlowRepository,
				Type:        "select",
				Options:     repoOptions,
			},
			{
				DisplayName: svc.t("dialog.flow.field.developer", nil),
				Name:        dialogFieldDeveloperProfile,
				Type:        "select",
				Default:     "developer",
				Options:     profileOptions,
			},
			{
				DisplayName: svc.t("dialog.flow.field.reviewer", nil),
				Name:        dialogFieldReviewerProfile,
				Type:        "select",
				Default:     "reviewer",
				Options:     profileOptions,
			},
			{
				DisplayName: svc.t("dialog.flow.field.title", nil),
				Name:        dialogFieldFlowTitle,
				Type:        "text",
				Placeholder: svc.t("dialog.flow.field.title.placeholder", nil),
				HelpText:    svc.t("dialog.flow.field.title.help", nil),
				MinLength:   3,
				MaxLength:   140,
			},
			{
				DisplayName: svc.t("dialog.flow.field.task", nil),
				Name:        dialogFieldFlowTask,
				Type:        "textarea",
				Placeholder: svc.t("dialog.flow.field.task.placeholder", nil),
				HelpText:    svc.t("dialog.flow.field.task.help", nil),
				MinLength:   3,
				MaxLength:   6000,
			},
			{
				DisplayName: svc.t("dialog.flow.field.max_attempts", nil),
				Name:        dialogFieldMaxAttempts,
				Type:        "select",
				Default:     strconv.Itoa(defaultFlowMaxAttempts),
				Options: []MattermostDialogOption{
					{Text: "1", Value: "1"},
					{Text: "2", Value: "2"},
					{Text: "3", Value: "3"},
				},
			},
		},
		SubmitLabel: svc.t("dialog.flow.start.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) runtimePruneApplyDialog(command MenuActionCommand) *MattermostDialog {
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackRuntimePruneApply,
		Title:            svc.t("dialog.runtime.prune.title", nil),
		IntroductionText: svc.t("dialog.runtime.prune.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.runtime.field.older_than", nil),
				Name:        dialogFieldOlderThan,
				Type:        "text",
				Default:     "24h",
				Placeholder: "24h",
				HelpText:    svc.t("dialog.runtime.field.older_than.help", nil),
				MinLength:   1,
				MaxLength:   16,
			},
			{
				DisplayName: svc.t("dialog.repo.field.confirm", nil),
				Name:        dialogFieldConfirm,
				Type:        "text",
				Placeholder: "apply",
				HelpText:    svc.t("dialog.runtime.field.confirm.help", nil),
				MinLength:   5,
				MaxLength:   5,
			},
		},
		SubmitLabel: svc.t("dialog.runtime.prune.submit", nil),
		State:       encodeDialogState(command),
	}
}

func (svc *SlashCommandService) handleRepositoryDialogUpsert(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState, requireExisting bool) DialogSubmissionResult {
	input, fieldErrors := svc.repositoryDialogUpsertInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("repo.add.storage_not_ready", nil)}
	}
	if requireExisting {
		if _, err := svc.cfg.Store.GetRepository(ctx, input.Provider, input.Owner, input.Name); err != nil {
			if errors.Is(err, adminrepo.ErrNotFound) {
				return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRepository: svc.t("dialog.repo.not_found", map[string]any{"Repository": input.Owner + "/" + input.Name})}}
			}
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})}
		}
	}
	text := svc.upsertRepositoryText(ctx, input, SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}, "repository metadata upserted from Mattermost dialog")
	return DialogSubmissionResult{
		StatusCode: 200,
		Card:       svc.dialogResultCard(ctx, state, command, text),
	}
}

func (svc *SlashCommandService) repositoryDialogUpsertInput(submission map[string]any) (adminrepo.UpsertRepositoryInput, map[string]string) {
	fieldErrors := map[string]string{}
	provider := strings.ToLower(defaultString(submissionString(submission, dialogFieldProvider), "github"))
	if provider != "github" {
		fieldErrors[dialogFieldProvider] = svc.t("dialog.repo.provider_invalid", nil)
	}
	owner, name, ok := parseSubmittedRepository(submissionString(submission, dialogFieldRepository))
	if !ok {
		fieldErrors[dialogFieldRepository] = svc.t("dialog.repo.repository_invalid", nil)
	}
	branch := defaultString(submissionString(submission, dialogFieldDefaultBranch), "main")
	if !validBranch(branch) {
		fieldErrors[dialogFieldDefaultBranch] = svc.t("dialog.repo.branch_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return adminrepo.UpsertRepositoryInput{}, fieldErrors
	}
	return adminrepo.UpsertRepositoryInput{
		Provider:          provider,
		Owner:             owner,
		Name:              name,
		DefaultBranch:     branch,
		GitHubAccountName: "primary",
	}, nil
}

func (svc *SlashCommandService) handleRepositoryDialogDelete(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	fieldErrors := map[string]string{}
	provider := strings.ToLower(defaultString(submissionString(command.Submission, dialogFieldProvider), "github"))
	if provider != "github" {
		fieldErrors[dialogFieldProvider] = svc.t("dialog.repo.provider_invalid", nil)
	}
	owner, name, ok := parseSubmittedRepository(submissionString(command.Submission, dialogFieldRepository))
	if !ok {
		fieldErrors[dialogFieldRepository] = svc.t("dialog.repo.repository_invalid", nil)
	}
	if submissionString(command.Submission, dialogFieldConfirm) != "delete" {
		fieldErrors[dialogFieldConfirm] = svc.t("dialog.repo.confirm_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("repo.list.storage_not_ready", nil)}
	}
	deleted, err := svc.cfg.Store.DeleteRepository(ctx, provider, owner, name)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRepository: svc.t("dialog.repo.not_found", map[string]any{"Repository": owner + "/" + name})}}
		}
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("repo.delete.failed", map[string]any{"Error": safeError(err)})}
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "repository.deleted",
		ActorUserID:  command.UserID,
		ActorUser:    defaultString(command.UserName, state.UserName),
		ResourceType: "repository",
		ResourceName: deleted.Provider + ":" + deleted.FullName(),
		Summary:      "repository metadata deleted from Mattermost dialog",
	})
	text := svc.t("repo.delete.result", map[string]any{
		"Provider": deleted.Provider,
		"FullName": deleted.FullName(),
	})
	return DialogSubmissionResult{
		StatusCode: 200,
		Card:       svc.dialogResultCard(ctx, state, command, text),
	}
}

func (svc *SlashCommandService) handleOpenAIAccountDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState, action string) DialogSubmissionResult {
	accountName, fieldErrors := svc.dialogAccountName(command.Submission)
	if action == openAIDialogActionDelete && submissionString(command.Submission, dialogFieldConfirm) != "delete" {
		fieldErrors[dialogFieldConfirm] = svc.t("dialog.repo.confirm_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	slashCommand := SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}
	var text string
	switch action {
	case openAIDialogActionAuth:
		text = svc.handleOpenAIAuth(ctx, []string{accountName}, slashCommand)
	case openAIDialogActionStatus:
		text = svc.handleOpenAIStatus(ctx, []string{accountName}, slashCommand)
	case openAIDialogActionCleanup:
		text = svc.handleOpenAICleanup(ctx, []string{accountName}, slashCommand)
	case openAIDialogActionDelete:
		text = svc.handleOpenAIDelete(ctx, []string{accountName}, slashCommand)
	default:
		return DialogSubmissionResult{StatusCode: 400, Error: svc.t("dialog.unknown", nil)}
	}
	return DialogSubmissionResult{
		StatusCode: 200,
		Card:       svc.dialogResultCard(ctx, state, command, text),
	}
}

func (svc *SlashCommandService) handleGitHubAccountDialogUpsert(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState, requireExisting bool) DialogSubmissionResult {
	input, fieldErrors := svc.githubAccountDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.list.storage_not_ready", nil)}
	}
	if svc.cfg.GitHubAccountInspector == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.inspect_not_configured", nil)}
	}
	if svc.cfg.RuntimeRunner == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.runtime_not_configured", nil)}
	}
	if requireExisting {
		if _, err := svc.cfg.Store.GetGitHubAccount(ctx, input.Name); err != nil {
			if errors.Is(err, adminrepo.ErrNotFound) {
				return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldAccount: svc.t("dialog.github.not_found", map[string]any{"Account": input.Name})}}
			}
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})}
		}
	}
	inspection, err := svc.cfg.GitHubAccountInspector.InspectToken(ctx, input.Token)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.inspect_failed", map[string]any{"Error": safeError(err)})}
	}
	scopes := strings.Join(inspection.Scopes, ", ")
	secretName := githubAccountSecretName(svc.cfg.GitHubSecretName, input.Name)
	secret, err := svc.cfg.RuntimeRunner.UpsertGitHubTokenSecret(ctx, runtimerepo.GitHubTokenSecretInput{
		AccountName: input.Name,
		SecretName:  secretName,
		Token:       input.Token,
		Username:    inspection.Username,
		Email:       inspection.Email,
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.secret_failed", map[string]any{"Error": safeError(err)})}
	}
	account, created, err := svc.cfg.Store.UpsertGitHubAccount(ctx, adminrepo.UpsertGitHubAccountInput{
		Name:           input.Name,
		CredentialName: githubCredentialName(input.Name),
		SecretRef:      secret.SecretName,
		Username:       inspection.Username,
		Email:          inspection.Email,
		Scopes:         scopes,
		Status:         "configured",
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("github.account.save_failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordGitHubAudit(ctx, SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}, "github.account.upserted", account.Name, "github account token inspected and secret upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("github.account.save_result", map[string]any{
		"State":         svc.t(stateID, nil),
		"Account":       account.Name,
		"Secret":        account.SecretRef,
		"Namespace":     secret.Namespace,
		"SecretCreated": secret.Created,
		"Username":      emptyAsUnknown(account.Username),
		"Email":         emptyAsUnknown(account.Email),
		"Scopes":        emptyAsUnknown(account.Scopes),
		"Status":        account.Status,
	})
	return DialogSubmissionResult{
		StatusCode: 200,
		Card:       svc.dialogResultCard(ctx, state, command, text),
	}
}

func (svc *SlashCommandService) handleRepositorySearchDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	query := strings.TrimSpace(submissionString(command.Submission, dialogFieldSearch))
	if len(query) < 2 {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldSearch: svc.t("dialog.repo.search.query_invalid", nil)}}
	}
	accountName, errText := svc.repositorySearchAccountName(ctx, state)
	if errText != "" {
		return DialogSubmissionResult{StatusCode: 200, Error: errText}
	}
	dialog, errText := svc.repositorySearchPickDialog(ctx, accountName, query, mattermostDialogCommand(state, command))
	if errText != "" {
		return DialogSubmissionResult{StatusCode: 200, Error: errText}
	}
	return DialogSubmissionResult{
		StatusCode: 200,
		Dialog:     dialog,
	}
}

func (svc *SlashCommandService) handleRepositorySearchPickDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	resourceID := submissionString(command.Submission, dialogFieldRepositoryChoice)
	repoState, ok := parseRepositoryOnboardingResourceID(resourceID)
	if !ok {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRepositoryChoice: svc.t("dialog.repo.repository_invalid", nil)}}
	}
	dialog, errText := svc.repositorySearchBranchDialog(ctx, repoState, mattermostDialogCommand(state, command))
	if errText != "" {
		return DialogSubmissionResult{StatusCode: 200, Error: errText}
	}
	return DialogSubmissionResult{
		StatusCode: 200,
		Dialog:     dialog,
	}
}

func (svc *SlashCommandService) handleRepositorySearchBranchDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	resourceID := submissionString(command.Submission, dialogFieldBranchChoice)
	if _, ok := parseRepositoryOnboardingResourceID(resourceID); !ok {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldBranchChoice: svc.t("dialog.repo.branch_invalid", nil)}}
	}
	card := svc.repositoryOnboardingConnectCard(ctx, MenuActionCommand{
		View:      state.View,
		Action:    menuActionRepositoryConnect,
		Resource:  menuResourceRepository,
		ID:        resourceID,
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
		PostID:    state.PostID,
	})
	return DialogSubmissionResult{
		StatusCode: 200,
		Card:       card,
	}
}

func (svc *SlashCommandService) repositorySearchPickDialog(ctx context.Context, accountName string, query string, command MenuActionCommand) (*MattermostDialog, string) {
	account, candidates, errText := svc.repositorySearchCandidates(ctx, accountName, query)
	if errText != "" {
		return nil, errText
	}
	options := make([]MattermostDialogOption, 0, len(candidates))
	limit := len(candidates)
	if limit > entityListPageSize {
		limit = entityListPageSize
	}
	for _, candidate := range candidates[:limit] {
		options = append(options, MattermostDialogOption{
			Text: repositorySearchRepositoryOptionText(candidate),
			Value: repositoryOnboardingResourceID(repositoryOnboardingState{
				ProjectID:     repositoryOnboardingProjectID(command),
				Account:       account.Name,
				Provider:      candidate.Provider,
				Owner:         candidate.Owner,
				Name:          candidate.Name,
				FullName:      candidate.FullName,
				DefaultBranch: candidate.DefaultBranch,
			}),
		})
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackRepositorySearchPick,
		Title:            svc.t("dialog.repo.pick.title", nil),
		IntroductionText: svc.t("dialog.repo.pick.intro", map[string]any{"Account": account.Name, "Query": query}),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.repo.field.repository", nil),
				Name:        dialogFieldRepositoryChoice,
				Type:        "select",
				HelpText:    svc.t("dialog.repo.pick.help", nil),
				Options:     options,
			},
		},
		SubmitLabel: svc.t("dialog.repo.pick.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) repositorySearchBranchDialog(ctx context.Context, state repositoryOnboardingState, command MenuActionCommand) (*MattermostDialog, string) {
	account, ok, errText := svc.repositoryOnboardingAccount(ctx, state.Account)
	if errText != "" {
		return nil, errText
	}
	if !ok {
		return nil, svc.t("repo.account.missing", map[string]any{"Account": state.Account})
	}
	if svc.cfg.GitHubRepositoryProvider == nil {
		return nil, svc.t("repo.onboard.provider_not_configured", nil)
	}
	branches, err := svc.cfg.GitHubRepositoryProvider.ListBranches(ctx, gitHubAccountRef(account), state.Owner, state.Name, 20)
	if err != nil {
		return nil, svc.t("repo.onboard.branches.failed", map[string]any{"Error": safeError(err)})
	}
	branchNames := repositoryOnboardingBranchNames(state.DefaultBranch, branches)
	if len(branchNames) == 0 {
		return nil, svc.t("repo.onboard.branches.empty", map[string]any{"Repository": state.FullName})
	}
	options := make([]MattermostDialogOption, 0, len(branchNames))
	limit := len(branchNames)
	if limit > entityListPageSize {
		limit = entityListPageSize
	}
	for _, branch := range branchNames[:limit] {
		next := state
		next.Branch = branch
		options = append(options, MattermostDialogOption{
			Text:  branch,
			Value: repositoryOnboardingResourceID(next),
		})
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackRepositorySearchBranch,
		Title:            svc.t("dialog.repo.branch.title", nil),
		IntroductionText: svc.t("dialog.repo.branch.intro", map[string]any{"Repository": state.FullName, "Account": account.Name}),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.repo.field.branch", nil),
				Name:        dialogFieldBranchChoice,
				Type:        "select",
				HelpText:    svc.t("dialog.repo.branch.help", nil),
				Options:     options,
			},
		},
		SubmitLabel: svc.t("dialog.repo.branch.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) repositorySearchCandidates(ctx context.Context, accountName string, query string) (entity.GitHubAccount, []providerrepo.RepositoryCandidate, string) {
	account, ok, errText := svc.repositoryOnboardingAccount(ctx, accountName)
	if errText != "" {
		return entity.GitHubAccount{}, nil, errText
	}
	if !ok {
		return entity.GitHubAccount{}, nil, svc.t("repo.account.missing", map[string]any{"Account": accountName})
	}
	if svc.cfg.GitHubRepositoryProvider == nil {
		return entity.GitHubAccount{}, nil, svc.t("repo.onboard.provider_not_configured", nil)
	}
	candidates, err := svc.repositoryCandidates(ctx, account, query)
	if err != nil {
		return entity.GitHubAccount{}, nil, svc.t("repo.onboard.repositories.failed", map[string]any{"Error": safeError(err)})
	}
	if len(candidates) == 0 {
		return entity.GitHubAccount{}, nil, svc.t("repo.onboard.repositories.empty", map[string]any{"Account": account.Name})
	}
	return account, candidates, ""
}

type githubAccountTokenDialogInput struct {
	Name  string
	Token string
}

func (svc *SlashCommandService) githubAccountDialogInput(submission map[string]any) (githubAccountTokenDialogInput, map[string]string) {
	fieldErrors := map[string]string{}
	accountName, err := parseOpenAIAccountName(submissionString(submission, dialogFieldAccount))
	if err != nil {
		fieldErrors[dialogFieldAccount] = svc.t("parse.github_account.invalid", nil)
	}
	token := submissionString(submission, dialogFieldToken)
	if strings.TrimSpace(token) == "" {
		fieldErrors[dialogFieldToken] = svc.t("dialog.github.token_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return githubAccountTokenDialogInput{}, fieldErrors
	}
	return githubAccountTokenDialogInput{Name: accountName, Token: token}, nil
}

func (svc *SlashCommandService) handleGitHubAccountDialogDelete(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	accountName, fieldErrors := svc.dialogAccountName(command.Submission)
	if submissionString(command.Submission, dialogFieldConfirm) != "delete" {
		fieldErrors[dialogFieldConfirm] = svc.t("dialog.repo.confirm_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	text := svc.deleteGitHubAccountText(ctx, accountName, SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	})
	return DialogSubmissionResult{
		StatusCode: 200,
		Card:       svc.dialogResultCard(ctx, state, command, text),
	}
}

func (svc *SlashCommandService) handleProfileDialogUpsert(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, fieldErrors := svc.profileDialogInput(command.Submission)
	if strings.TrimSpace(state.ResourceID) != "" && input.Name != state.ResourceID {
		fieldErrors[dialogFieldProfile] = svc.t("dialog.profile.rename_not_supported", nil)
	}
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("profile.list.storage_not_ready", nil)}
	}
	if strings.TrimSpace(state.ResourceID) != "" {
		existing, err := svc.cfg.Store.GetAgentProfile(ctx, state.ResourceID)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("dev.profile_not_ready", map[string]any{"Profile": state.ResourceID})}
		}
		input.Enabled = existing.Enabled
	}
	if _, err := svc.cfg.Store.GetOpenAIAccount(ctx, input.OpenAIAccountName); err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldOpenAIAccount: svc.t("openai.status.account_not_found", map[string]any{"Account": input.OpenAIAccountName})}}
	}
	if _, err := svc.cfg.Store.GetGitHubAccount(ctx, input.GitHubAccountName); err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldGitHubAccount: svc.t("dialog.github.not_found", map[string]any{"Account": input.GitHubAccountName})}}
	}
	profile, created, err := svc.cfg.Store.UpsertAgentProfile(ctx, input)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("profile.save.failed", map[string]any{"Error": safeError(err)})}
	}
	seeded, err := svc.ensurePromptTemplatesForProfile(ctx, profile)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("profile.save.prompt_seed_failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProfileAudit(ctx, SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}, "agent_profile.upserted", profile.Name, "agent profile upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("profile.save.result", map[string]any{
		"State":            svc.t(stateID, nil),
		"Profile":          profile.Name,
		"Role":             profile.Role,
		"OpenAI":           defaultString(profile.OpenAIAccountName, "primary"),
		"GitHub":           defaultString(profile.GitHubAccountName, "primary"),
		"KubernetesAccess": defaultString(profile.KubernetesAccess, "read-only"),
		"Sandbox":          defaultString(profile.SandboxMode, "danger-full-access"),
		"Seeded":           seeded,
	})
	return DialogSubmissionResult{StatusCode: 200, Card: svc.dialogResultCard(ctx, state, command, text)}
}

func (svc *SlashCommandService) handlePromptEditDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	profileName, templateKey, ok := parsePromptTemplateResourceID(state.ResourceID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("menu.entity.invalid", nil)}
	}
	body := strings.TrimSpace(submissionString(command.Submission, dialogFieldTemplateBody))
	if body == "" {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldTemplateBody: svc.t("dialog.prompt.body_empty", nil)}}
	}
	rendered, err := renderAgentPromptTemplate(body, samplePromptTemplateData(profileName, templateKey, svc.promptTemplateLocaleData()))
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldTemplateBody: svc.t("prompt.set.render_failed", map[string]any{"Error": safeError(err)})}}
	}
	item, created, err := svc.cfg.Store.UpsertAgentPromptTemplate(ctx, adminrepo.UpsertAgentPromptTemplateInput{
		ProfileName: profileName,
		TemplateKey: templateKey,
		Body:        body,
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("prompt.set.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordPromptAudit(ctx, SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}, "agent_prompt_template.upserted", item.ProfileName+"/"+item.TemplateKey, "agent prompt template upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("prompt.set.result", map[string]any{
		"State":       svc.t(stateID, nil),
		"Profile":     item.ProfileName,
		"TemplateKey": item.TemplateKey,
		"Rendered":    sanitizeLogTail(rendered),
	})
	return DialogSubmissionResult{StatusCode: 200, Card: svc.dialogResultCard(ctx, state, command, text)}
}

func (svc *SlashCommandService) handleFlowStartDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, fieldErrors := svc.flowStartDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if svc.cfg.RuntimeRunner == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("runtime.not_configured", nil)}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("flow.storage_not_ready", nil)}
	}
	provider, owner, name, ok := parseRepositoryResourceID(input.RepositoryID)
	if !ok || provider != "github" {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldFlowRepository: svc.t("dialog.repo.repository_invalid", nil)}}
	}
	repo, err := svc.cfg.Store.GetRepository(ctx, provider, owner, name)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldFlowRepository: svc.t("dialog.repo.not_found", map[string]any{"Repository": owner + "/" + name})}}
	}
	if _, message, ok := svc.flowAgentRuntime(ctx, input.DeveloperProfile); !ok {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldDeveloperProfile: message}}
	}
	if _, message, ok := svc.flowAgentRuntime(ctx, input.ReviewerProfile); !ok {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldReviewerProfile: message}}
	}
	flowID := newFlowID(repo.Name)
	flow, created, err := svc.cfg.Store.CreateAgentFlow(ctx, adminrepo.CreateAgentFlowInput{
		FlowID:               flowID,
		Status:               flowStatusCreated,
		Provider:             repo.Provider,
		Owner:                repo.Owner,
		Name:                 repo.Name,
		BaseBranch:           defaultString(repo.DefaultBranch, "main"),
		HeadBranch:           flowHeadBranch(flowID),
		Title:                input.Title,
		Task:                 input.Task,
		Attempt:              1,
		MaxAttempts:          input.MaxAttempts,
		DeveloperProfileName: input.DeveloperProfile,
		ReviewerProfileName:  input.ReviewerProfile,
		FlowPreset:           "developer_review",
		OwnerUserID:          strings.TrimSpace(command.UserID),
		OwnerUser:            defaultString(command.UserName, state.UserName),
		ActionToken:          newFlowActionToken(),
		Summary:              "developer-review flow created from Mattermost dialog",
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("flow.start.store_failed", map[string]any{"Error": safeError(err)})}
	}
	if !created {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("flow.start.exists", map[string]any{"FlowID": flow.FlowID, "Status": flow.Status})}
	}
	flow, started, err := svc.startFlowDeveloperAttempt(ctx, flow, false)
	if err != nil {
		_, _ = svc.cfg.Store.UpdateAgentFlow(ctx, adminrepo.UpdateAgentFlowInput{FlowID: flow.FlowID, Status: flowStatusBlocked, Summary: safeError(err)})
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("flow.start.failed", map[string]any{"Error": safeError(err)})}
	}
	cardLine := svc.t("flow.start.card_skipped", nil)
	slashCommand := SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}
	if slashCommand.ChannelID != "" && svc.cfg.FlowCardPublisher != nil && svc.cfg.FlowActionURL != "" {
		updated, post, err := svc.publishFlowCard(ctx, flow, slashCommand)
		if err != nil {
			cardLine = svc.t("flow.start.card_failed", map[string]any{"Error": safeError(err)})
		} else {
			flow = updated
			cardLine = svc.t("flow.start.card_posted", map[string]any{"PostID": post.PostID})
		}
	}
	svc.recordFlowAudit(ctx, slashCommand, "flow.started", flow.FlowID, "developer-review flow started from Mattermost dialog")
	text := svc.t("flow.start.started", map[string]any{
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
	return DialogSubmissionResult{StatusCode: 200, Card: svc.dialogResultCard(ctx, state, command, text)}
}

func (svc *SlashCommandService) handleRuntimePruneDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	fieldErrors := map[string]string{}
	olderThan := strings.TrimSpace(submissionString(command.Submission, dialogFieldOlderThan))
	if olderThan == "" {
		fieldErrors[dialogFieldOlderThan] = svc.t("runtime.prune.usage", nil)
	}
	if submissionString(command.Submission, dialogFieldConfirm) != "apply" {
		fieldErrors[dialogFieldConfirm] = svc.t("dialog.runtime.confirm_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	text := svc.handleRuntimePrune(ctx, []string{olderThan, "--apply"}, SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	})
	return DialogSubmissionResult{StatusCode: 200, Card: svc.dialogResultCard(ctx, state, command, text)}
}

func (svc *SlashCommandService) profileDialogInput(submission map[string]any) (adminrepo.UpsertAgentProfileInput, map[string]string) {
	fieldErrors := map[string]string{}
	name, err := parseOpenAIAccountName(submissionString(submission, dialogFieldProfile))
	if err != nil {
		fieldErrors[dialogFieldProfile] = svc.t("parse.profile.invalid", nil)
	}
	role := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldRole)))
	if !validProfileRole(role) {
		fieldErrors[dialogFieldRole] = svc.t("dialog.profile.role_invalid", nil)
	}
	openAIAccount, err := parseOpenAIAccountName(submissionString(submission, dialogFieldOpenAIAccount))
	if err != nil {
		fieldErrors[dialogFieldOpenAIAccount] = svc.t("parse.openai_account.invalid", nil)
	}
	githubAccount, err := parseOpenAIAccountName(submissionString(submission, dialogFieldGitHubAccount))
	if err != nil {
		fieldErrors[dialogFieldGitHubAccount] = svc.t("parse.github_account.invalid", nil)
	}
	kubernetesAccess := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldKubernetesAccess)))
	if !validKubernetesAccess(kubernetesAccess) {
		fieldErrors[dialogFieldKubernetesAccess] = svc.t("dialog.profile.kubernetes_invalid", nil)
	}
	sandboxMode := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldSandboxMode)))
	if !validSandboxMode(sandboxMode) {
		fieldErrors[dialogFieldSandboxMode] = svc.t("dialog.profile.sandbox_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return adminrepo.UpsertAgentProfileInput{}, fieldErrors
	}
	return adminrepo.UpsertAgentProfileInput{
		Name:              name,
		Role:              role,
		Description:       strings.TrimSpace(submissionString(submission, dialogFieldDescription)),
		Enabled:           true,
		OpenAIAccountName: openAIAccount,
		GitHubAccountName: githubAccount,
		KubernetesAccess:  kubernetesAccess,
		SandboxMode:       sandboxMode,
		ConfigOverlay:     strings.TrimSpace(submissionString(submission, dialogFieldConfigOverlay)),
	}, nil
}

type flowStartDialogInput struct {
	RepositoryID     string
	DeveloperProfile string
	ReviewerProfile  string
	Title            string
	Task             string
	MaxAttempts      int
}

func (svc *SlashCommandService) flowStartDialogInput(submission map[string]any) (flowStartDialogInput, map[string]string) {
	fieldErrors := map[string]string{}
	repositoryID := strings.TrimSpace(submissionString(submission, dialogFieldFlowRepository))
	if _, _, _, ok := parseRepositoryResourceID(repositoryID); !ok {
		fieldErrors[dialogFieldFlowRepository] = svc.t("dialog.repo.repository_invalid", nil)
	}
	developerProfile := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldDeveloperProfile)))
	if !validPromptTemplateID(developerProfile) {
		fieldErrors[dialogFieldDeveloperProfile] = svc.t("prompt.invalid_id", nil)
	}
	reviewerProfile := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldReviewerProfile)))
	if !validPromptTemplateID(reviewerProfile) {
		fieldErrors[dialogFieldReviewerProfile] = svc.t("prompt.invalid_id", nil)
	}
	title := strings.TrimSpace(submissionString(submission, dialogFieldFlowTitle))
	if len(title) < 3 {
		fieldErrors[dialogFieldFlowTitle] = svc.t("dialog.flow.title_invalid", nil)
	}
	task := strings.TrimSpace(submissionString(submission, dialogFieldFlowTask))
	if len(task) < 3 {
		fieldErrors[dialogFieldFlowTask] = svc.t("dialog.flow.task_invalid", nil)
	}
	maxAttempts, err := strconv.Atoi(defaultString(submissionString(submission, dialogFieldMaxAttempts), strconv.Itoa(defaultFlowMaxAttempts)))
	if err != nil || maxAttempts < 1 || maxAttempts > defaultFlowMaxAttempts {
		fieldErrors[dialogFieldMaxAttempts] = svc.t("dialog.flow.max_attempts_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return flowStartDialogInput{}, fieldErrors
	}
	return flowStartDialogInput{
		RepositoryID:     repositoryID,
		DeveloperProfile: developerProfile,
		ReviewerProfile:  reviewerProfile,
		Title:            title,
		Task:             task,
		MaxAttempts:      maxAttempts,
	}, nil
}

func (svc *SlashCommandService) dialogAccountName(submission map[string]any) (string, map[string]string) {
	accountName, err := parseOpenAIAccountName(submissionString(submission, dialogFieldAccount))
	if err != nil {
		return "", map[string]string{dialogFieldAccount: svc.t("parse.openai_account.invalid", nil)}
	}
	return accountName, nil
}

func (svc *SlashCommandService) dialogResultCard(ctx context.Context, state mattermostDialogState, command DialogSubmissionCommand, text string) *MattermostCard {
	view := normalizeMenuView(defaultString(state.View, menuViewRepositories))
	card := svc.menuCommandResultCard(ctx, view, "", text)
	card.ChannelID = defaultString(state.ChannelID, command.ChannelID)
	card.PostID = state.PostID
	return card
}

func mattermostDialogCommand(state mattermostDialogState, command DialogSubmissionCommand) MenuActionCommand {
	return MenuActionCommand{
		View:      state.View,
		ID:        state.ResourceID,
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
		PostID:    state.PostID,
	}
}

func (svc *SlashCommandService) menuCard(ctx context.Context, view string) *MattermostCard {
	view = normalizeMenuView(view)
	card := &MattermostCard{
		ActionURL: svc.cfg.MenuActionURL,
		Message:   svc.t("menu.message", nil),
		Color:     "#1c58d9",
		Title:     svc.t("menu."+view+".title", nil),
		Text:      svc.menuText(view),
		Fields:    svc.menuFields(ctx, view),
		Actions:   svc.menuActions(view),
	}
	if view != menuViewMain {
		card.Color = "#5b667a"
	}
	return card
}

func (svc *SlashCommandService) menuCommandResultCard(ctx context.Context, view string, command string, resultText string) *MattermostCard {
	view = normalizeMenuView(view)
	text := resultText
	if menuCommandPrivateOutput(command) {
		text = svc.t("menu.command_result.private_text", nil)
	}
	return &MattermostCard{
		ActionURL: svc.cfg.MenuActionURL,
		Message:   svc.t("menu.message", nil),
		Color:     "#227a55",
		Title: svc.t("menu.command_result.title", map[string]any{
			"Section": svc.t("menu."+view+".title", nil),
		}),
		Text: text,
		Fields: []MattermostCardField{
			{Title: svc.t("menu.field.current", nil), Value: svc.t("menu."+view+".breadcrumb", nil), Short: true},
		},
		Actions: []MattermostCardAction{
			svc.menuAction(view, "menu.action.back", "menu.action.back.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
		},
	}
}

func menuCommandPrivateOutput(command string) bool {
	command = strings.TrimSpace(command)
	return strings.HasPrefix(command, "openai auth ") || strings.HasPrefix(command, "openai status ")
}

func (svc *SlashCommandService) menuText(view string) string {
	switch view {
	case menuViewProjects:
		return svc.t("menu.projects.text", nil)
	case menuViewStartFlow:
		return svc.t("menu.start_flow.text", nil)
	case menuViewPending:
		return svc.t("menu.pending.text", nil)
	case menuViewRepositories:
		return svc.t("menu.repositories.text", nil)
	case menuViewAccounts:
		return svc.t("menu.accounts.text", nil)
	case menuViewOpenAI:
		return svc.t("menu.openai.text", nil)
	case menuViewGitHub:
		return svc.t("menu.github.text", nil)
	case menuViewRoles:
		return svc.t("menu.roles.text", nil)
	case menuViewChats:
		return svc.t("menu.chats.text", nil)
	case menuViewProfiles:
		return svc.t("menu.profiles.text", nil)
	case menuViewPrompts:
		return svc.t("menu.prompts.text", nil)
	case menuViewRuntime:
		return svc.t("menu.runtime.text", nil)
	case menuViewSystem:
		return svc.t("menu.system.text", nil)
	case menuViewAdvanced:
		return svc.t("menu.advanced.text", nil)
	case menuViewHelp:
		return svc.t("menu.help.text", nil)
	default:
		return svc.t("menu.main.text", nil)
	}
}

func (svc *SlashCommandService) menuFields(ctx context.Context, view string) []MattermostCardField {
	if view != menuViewMain {
		return []MattermostCardField{
			{Title: svc.t("menu.field.current", nil), Value: svc.t("menu."+view+".breadcrumb", nil), Short: true},
			{Title: svc.t("menu.field.next", nil), Value: svc.t("menu."+view+".next", nil), Short: true},
		}
	}
	return []MattermostCardField{
		{Title: svc.t("menu.field.projects", nil), Value: svc.projectsStatusText(ctx), Short: true},
		{Title: svc.t("menu.field.storage", nil), Value: readyLabel(svc.cfg.Localizer, svc.cfg.StorageReady), Short: true},
		{Title: svc.t("menu.field.runtime", nil), Value: configuredLabel(svc.cfg.Localizer, svc.cfg.RuntimeConfigured), Short: true},
		{Title: svc.t("menu.field.github", nil), Value: svc.githubAccountsStatusText(ctx), Short: true},
		{Title: svc.t("menu.field.openai", nil), Value: svc.openAIAccountsStatusText(ctx), Short: true},
		{Title: svc.t("menu.field.locale", nil), Value: "`" + svc.cfg.Localizer.Locale() + "`", Short: true},
	}
}

func (svc *SlashCommandService) githubAccountsStatusText(ctx context.Context) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return configuredLabel(svc.cfg.Localizer, svc.cfg.GitHubTokenConfigured)
	}
	accounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		return readyLabel(svc.cfg.Localizer, false)
	}
	configured := 0
	for _, account := range accounts {
		if account.Status == "configured" {
			configured++
		}
	}
	return fmt.Sprintf("`%d/%d`", configured, len(accounts))
}

func (svc *SlashCommandService) openAIAccountsStatusText(ctx context.Context) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return readyLabel(svc.cfg.Localizer, false)
	}
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		return readyLabel(svc.cfg.Localizer, false)
	}
	authorized := 0
	for _, account := range accounts {
		if account.Status == "authorized" {
			authorized++
		}
	}
	return fmt.Sprintf("`%d/%d`", authorized, len(accounts))
}

func (svc *SlashCommandService) menuActions(view string) []MattermostCardAction {
	switch view {
	case menuViewMain:
		return []MattermostCardAction{
			svc.menuAction(menuViewProjects, "menu.action.projects", "menu.action.projects.tooltip", "primary"),
			svc.menuAction(menuViewAccounts, "menu.action.accounts", "menu.action.accounts.tooltip", "default"),
			svc.menuAction(menuViewRepositories, "menu.action.repositories", "menu.action.repositories.tooltip", "default"),
			svc.menuAction(menuViewRoles, "menu.action.roles", "menu.action.roles.tooltip", "default"),
			svc.menuAction(menuViewChats, "menu.action.chats", "menu.action.chats.tooltip", "default"),
			svc.menuAction(menuViewRuntime, "menu.action.runtime", "menu.action.runtime.tooltip", "default"),
			svc.menuAction(menuViewSystem, "menu.action.system", "menu.action.system.tooltip", "default"),
			svc.menuAction(menuViewAdvanced, "menu.action.advanced", "menu.action.advanced.tooltip", "default"),
		}
	case menuViewProjects:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewProjects, "projectlist", menuActionList, menuResourceProject, "", "menu.action.project_list", "menu.action.project_list.tooltip", "primary", nil),
			svc.menuDialogAction(menuViewProjects, "dialogprojectadd", menuDialogProjectUpsert, "menu.action.project_add", "menu.action.project_add.tooltip", "primary"),
			svc.menuAction(menuViewAccounts, "menu.action.accounts", "menu.action.accounts.tooltip", "default"),
			svc.menuAction(menuViewRepositories, "menu.action.repositories", "menu.action.repositories.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewAccounts:
		return []MattermostCardAction{
			svc.menuAction(menuViewOpenAI, "menu.action.openai", "menu.action.openai.tooltip", "primary"),
			svc.menuAction(menuViewGitHub, "menu.action.github", "menu.action.github.tooltip", "primary"),
			svc.menuAction(menuViewSystem, "menu.action.system", "menu.action.system.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewOpenAI:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewOpenAI, "openailist", menuActionList, menuResourceOpenAIAccount, "", "menu.action.openai_list", "menu.action.openai_list.tooltip", "primary", nil),
			svc.menuDialogAction(menuViewOpenAI, "dialogopenaiauth", menuDialogOpenAIAuth, "menu.action.openai_auth", "menu.action.openai_auth.tooltip", "default"),
			svc.menuAction(menuViewAccounts, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewGitHub:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewGitHub, "githublist", menuActionList, menuResourceGitHubAccount, "", "menu.action.github_account_list", "menu.action.github_account_list.tooltip", "primary", nil),
			svc.menuDialogAction(menuViewGitHub, "dialoggithubadd", menuDialogGitHubAccountAdd, "menu.action.github_account_add", "menu.action.github_account_add.tooltip", "primary"),
			svc.menuAction(menuViewAccounts, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewStartFlow:
		return []MattermostCardAction{
			svc.menuDialogAction(menuViewStartFlow, "dialogflowstart", menuDialogFlowStart, "menu.action.flow_start", "menu.action.flow_start.tooltip", "primary"),
			svc.menuAction(menuViewPending, "menu.action.pending", "menu.action.pending.tooltip", "warning"),
			svc.menuResourceAction(menuViewPending, "flowlist", menuActionList, menuResourceFlow, "", "menu.action.flow_list", "menu.action.flow_list.tooltip", "default", nil),
			svc.menuAction(menuViewRepositories, "menu.action.repositories", "menu.action.repositories.tooltip", "default"),
			svc.menuAction(menuViewProfiles, "menu.action.profiles", "menu.action.profiles.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewPending:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewPending, "pendinglist", menuActionList, menuResourceFlow, "", "menu.action.pending_list", "menu.action.pending_list.tooltip", "primary", nil),
			svc.menuAction(menuViewStartFlow, "menu.action.start_flow", "menu.action.start_flow.tooltip", "primary"),
			svc.menuAction(menuViewRepositories, "menu.action.repositories", "menu.action.repositories.tooltip", "default"),
			svc.menuAction(menuViewProfiles, "menu.action.profiles", "menu.action.profiles.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewRepositories:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewRepositories, "repoonboard", menuActionRepositoryOnboard, menuResourceRepository, "", "menu.action.repo_add", "menu.action.repo_add.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewRepositories, "repolist", menuActionList, menuResourceRepository, "", "menu.action.repo_list", "menu.action.repo_list.tooltip", "primary", nil),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewRoles:
		return []MattermostCardAction{
			svc.menuDialogAction(menuViewRoles, "dialogroleadd", menuDialogAgentRoleUpsert, "menu.action.role_add", "menu.action.role_add.tooltip", "primary"),
			svc.menuResourceAction(menuViewRoles, "rolelist", menuActionList, menuResourceAgentRole, "", "menu.action.role_list", "menu.action.role_list.tooltip", "primary", nil),
			svc.menuAction(menuViewProjects, "menu.action.projects", "menu.action.projects.tooltip", "default"),
			svc.menuAction(menuViewAccounts, "menu.action.accounts", "menu.action.accounts.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewChats:
		return []MattermostCardAction{
			svc.menuDialogAction(menuViewChats, "dialogchatcreate", menuDialogChatCreate, "menu.action.chat_add", "menu.action.chat_add.tooltip", "primary"),
			svc.menuResourceAction(menuViewChats, "chatlist", menuActionList, menuResourceChat, "", "menu.action.chat_list", "menu.action.chat_list.tooltip", "primary", nil),
			svc.menuAction(menuViewProjects, "menu.action.projects", "menu.action.projects.tooltip", "default"),
			svc.menuAction(menuViewRoles, "menu.action.roles", "menu.action.roles.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewProfiles:
		return []MattermostCardAction{
			svc.menuDialogAction(menuViewProfiles, "dialogprofileadd", menuDialogProfileUpsert, "menu.action.profile_add", "menu.action.profile_add.tooltip", "primary"),
			svc.menuResourceAction(menuViewProfiles, "profilelist", menuActionList, menuResourceProfile, "", "menu.action.profile_list", "menu.action.profile_list.tooltip", "primary", nil),
			svc.menuAction(menuViewOpenAI, "menu.action.openai", "menu.action.openai.tooltip", "default"),
			svc.menuAction(menuViewGitHub, "menu.action.github", "menu.action.github.tooltip", "default"),
			svc.menuAction(menuViewPrompts, "menu.action.prompts", "menu.action.prompts.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewPrompts:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewPrompts, "promptlist", menuActionList, menuResourcePromptTemplate, "", "menu.action.prompt_list", "menu.action.prompt_list.tooltip", "primary", nil),
			svc.menuAction(menuViewProfiles, "menu.action.profiles", "menu.action.profiles.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewRuntime:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewRuntime, "runtimeruns", menuActionList, menuResourceRun, "", "menu.action.runtime_runs", "menu.action.runtime_runs.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewRuntime, "runtimesmoke", menuActionRuntimeSmoke, menuResourceRuntime, "", "menu.action.runtime_smoke", "menu.action.runtime_smoke.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewRuntime, "runtimeprunedryrun", menuActionRuntimePruneDry, menuResourceRuntime, "", "menu.action.runtime_prune_dry_run", "menu.action.runtime_prune_dry_run.tooltip", "default", nil),
			svc.menuDialogAction(menuViewRuntime, "dialogruntimeprune", menuDialogRuntimePruneApply, "menu.action.runtime_prune_apply", "menu.action.runtime_prune_apply.tooltip", "warning"),
			svc.menuAction(menuViewSystem, "menu.action.system", "menu.action.system.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewSystem:
		return []MattermostCardAction{
			svc.menuResourceAction(menuViewSystem, "systemstatus", menuActionSystemStatus, menuResourceSystem, "", "menu.action.status", "menu.action.status.tooltip", "primary", nil),
			svc.menuResourceAction(menuViewSystem, "tokencheck", menuActionTokenCheck, menuResourceSystem, "", "menu.action.token_check", "menu.action.token_check.tooltip", "default", nil),
			svc.menuResourceAction(menuViewSystem, "localeget", menuActionLocaleGet, menuResourceSystem, "", "menu.action.locale_get", "menu.action.locale_get.tooltip", "default", nil),
			svc.menuResourceAction(menuViewSystem, "localesetru", menuActionLocaleSetRU, menuResourceSystem, "", "menu.action.locale_set_ru", "menu.action.locale_set_ru.tooltip", "default", nil),
			svc.menuResourceAction(menuViewSystem, "localeseten", menuActionLocaleSetEN, menuResourceSystem, "", "menu.action.locale_set_en", "menu.action.locale_set_en.tooltip", "default", nil),
			svc.menuAction(menuViewAdvanced, "menu.action.advanced", "menu.action.advanced.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewAdvanced:
		return []MattermostCardAction{
			svc.menuAction(menuViewStartFlow, "menu.action.start_flow", "menu.action.start_flow.tooltip", "default"),
			svc.menuAction(menuViewPending, "menu.action.pending", "menu.action.pending.tooltip", "warning"),
			svc.menuAction(menuViewProfiles, "menu.action.profiles", "menu.action.profiles.tooltip", "default"),
			svc.menuAction(menuViewPrompts, "menu.action.prompts", "menu.action.prompts.tooltip", "default"),
			svc.menuAction(menuViewRuntime, "menu.action.runtime", "menu.action.runtime.tooltip", "default"),
			svc.menuAction(menuViewSystem, "menu.action.system", "menu.action.system.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	case menuViewHelp:
		return []MattermostCardAction{
			svc.menuAction(menuViewProjects, "menu.action.projects", "menu.action.projects.tooltip", "primary"),
			svc.menuAction(menuViewAccounts, "menu.action.accounts", "menu.action.accounts.tooltip", "default"),
			svc.menuAction(menuViewRoles, "menu.action.roles", "menu.action.roles.tooltip", "default"),
			svc.menuAction(menuViewChats, "menu.action.chats", "menu.action.chats.tooltip", "default"),
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
		}
	default:
		return []MattermostCardAction{
			svc.menuAction(menuViewMain, "menu.action.back", "menu.action.back.tooltip", "default"),
			svc.menuAction(menuViewProjects, "menu.action.projects", "menu.action.projects.tooltip", "primary"),
			svc.menuAction(menuViewSystem, "menu.action.system", "menu.action.system.tooltip", "default"),
		}
	}
}

func (svc *SlashCommandService) menuDialogAction(view string, actionID string, dialog string, nameID string, tooltipID string, style string) MattermostCardAction {
	action := svc.menuAction(view, nameID, tooltipID, style)
	action.ID = actionID
	action.Context["dialog"] = dialog
	return action
}

func (svc *SlashCommandService) menuAction(view string, nameID string, tooltipID string, style string) MattermostCardAction {
	return MattermostCardAction{
		ID:      "menu" + strings.ReplaceAll(view, "_", ""),
		Name:    svc.t(nameID, nil),
		Tooltip: svc.t(tooltipID, nil),
		Style:   style,
		Context: map[string]any{
			"kind": "agents_menu",
			"view": view,
		},
	}
}

func (svc *SlashCommandService) handleGitHub(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return svc.t("github.usage", nil)
	}
	if args[0] == "account" {
		return svc.handleGitHubAccount(ctx, args[1:])
	}
	if svc.cfg.RepositoryProvider == nil {
		return svc.t("github.provider_not_configured", nil)
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

func (svc *SlashCommandService) handleGitHubAccount(ctx context.Context, args []string) string {
	if len(args) == 0 {
		return svc.t("github.account.usage", nil)
	}
	switch args[0] {
	case "list":
		return svc.handleGitHubAccountList(ctx)
	default:
		return svc.t("github.account.unknown_command", nil)
	}
}

func (svc *SlashCommandService) handleGitHubAccountList(ctx context.Context) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("github.account.list.storage_not_ready", nil)
	}
	accounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		return svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(accounts) == 0 {
		return svc.t("github.account.list.empty", nil)
	}
	lines := []string{svc.t("github.account.list.header", nil)}
	for _, account := range accounts {
		lines = append(lines, svc.t("github.account.list.item", map[string]any{
			"Account":  account.Name,
			"Status":   account.Status,
			"Secret":   account.SecretRef,
			"Username": emptyAsUnknown(account.Username),
			"Email":    emptyAsUnknown(account.Email),
			"Scopes":   emptyAsUnknown(account.Scopes),
		}))
	}
	return strings.Join(lines, "\n")
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
	return svc.repositoryWebhookRegistrationLines(registration)
}

func (svc *SlashCommandService) ensureRepositoryWebhookLineForRepository(ctx context.Context, command SlashCommand, repo entity.Repository) []string {
	if repo.Provider != "github" {
		return nil
	}
	if !svc.cfg.GitHubWebhookConfigured {
		return []string{svc.t("webhook.skipped_secret", nil)}
	}
	account, ok, errText := svc.repositoryGitHubAccount(ctx, repo)
	if errText != "" {
		return []string{errText}
	}
	if ok && svc.cfg.GitHubRepositoryProvider != nil {
		registration, err := svc.cfg.GitHubRepositoryProvider.EnsureRepositoryWebhook(ctx, gitHubAccountRef(account), repo.Owner, repo.Name)
		if err != nil {
			return []string{svc.t("webhook.not_registered_error", map[string]any{"Error": safeError(err)})}
		}
		svc.recordGitHubAudit(ctx, command, "github.webhook.ensured", repo.Owner+"/"+repo.Name, "github repository webhook ensured with account")
		return svc.repositoryWebhookRegistrationLines(registration)
	}
	if svc.cfg.RepositoryProvider == nil {
		return []string{svc.t("webhook.skipped_provider", nil)}
	}
	registration, err := svc.cfg.RepositoryProvider.EnsureRepositoryWebhook(ctx, repo.Owner, repo.Name)
	if err != nil {
		return []string{svc.t("webhook.not_registered_error", map[string]any{"Error": safeError(err)})}
	}
	svc.recordGitHubAudit(ctx, command, "github.webhook.ensured", repo.Owner+"/"+repo.Name, "github repository webhook ensured")
	return svc.repositoryWebhookRegistrationLines(registration)
}

func (svc *SlashCommandService) repositoryWebhookRegistrationLines(registration providerrepo.WebhookRegistration) []string {
	action := svc.t("label.updated", nil)
	if registration.Created {
		action = svc.t("label.created", nil)
	}
	return []string{
		svc.t("webhook.result", map[string]any{"Action": action, "ID": registration.ID, "Active": registration.Active}),
		svc.t("webhook.events", map[string]any{"Events": strings.Join(registration.Events, "`, `")}),
	}
}

func (svc *SlashCommandService) repositoryGitHubCheckText(ctx context.Context, command MenuActionCommand, provider string, owner string, name string) string {
	repo, ok, errText := svc.repositoryFromMenu(ctx, provider, owner, name)
	if errText != "" {
		return errText
	}
	if !ok {
		return svc.t("menu.entity.invalid", nil)
	}
	account, accountOK, errText := svc.repositoryGitHubAccount(ctx, repo)
	if errText != "" {
		return errText
	}
	if accountOK && svc.cfg.GitHubRepositoryProvider != nil {
		access, err := svc.cfg.GitHubRepositoryProvider.CheckRepository(ctx, gitHubAccountRef(account), repo.Owner, repo.Name)
		if err != nil {
			return svc.t("github.check.failed", map[string]any{"Error": safeError(err)})
		}
		svc.recordGitHubAudit(ctx, svc.slashFromMenu(command), "github.repository.checked", access.Owner+"/"+access.Name, "github repository access checked with account")
		return svc.repositoryAccessText(access)
	}
	if svc.cfg.RepositoryProvider == nil {
		return svc.t("github.provider_not_configured", nil)
	}
	access, err := svc.cfg.RepositoryProvider.CheckRepository(ctx, repo.Owner, repo.Name)
	if err != nil {
		return svc.t("github.check.failed", map[string]any{"Error": safeError(err)})
	}
	svc.recordGitHubAudit(ctx, svc.slashFromMenu(command), "github.repository.checked", access.Owner+"/"+access.Name, "github repository access checked")
	return svc.repositoryAccessText(access)
}

func (svc *SlashCommandService) repositoryGitHubWebhookText(ctx context.Context, command MenuActionCommand, provider string, owner string, name string) string {
	repo, ok, errText := svc.repositoryFromMenu(ctx, provider, owner, name)
	if errText != "" {
		return errText
	}
	if !ok {
		return svc.t("menu.entity.invalid", nil)
	}
	lines := svc.ensureRepositoryWebhookLineForRepository(ctx, svc.slashFromMenu(command), repo)
	if len(lines) == 0 {
		return svc.t("github.webhook.not_registered", nil)
	}
	return svc.t("github.webhook.header", nil) + "\n" + strings.Join(lines, "\n")
}

func (svc *SlashCommandService) repositoryFromMenu(ctx context.Context, provider string, owner string, name string) (entity.Repository, bool, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.Repository{}, false, svc.t("repo.list.storage_not_ready", nil)
	}
	repo, err := svc.cfg.Store.GetRepository(ctx, provider, owner, name)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return entity.Repository{}, false, ""
		}
		return entity.Repository{}, false, svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})
	}
	return repo, true, ""
}

func (svc *SlashCommandService) repositoryGitHubAccount(ctx context.Context, repo entity.Repository) (entity.GitHubAccount, bool, string) {
	accountName := defaultString(repo.GitHubAccountName, "primary")
	if svc.cfg.Store == nil || !svc.cfg.StorageReady {
		return entity.GitHubAccount{}, false, ""
	}
	account, err := svc.cfg.Store.GetGitHubAccount(ctx, accountName)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return entity.GitHubAccount{}, false, svc.t("repo.account.missing", map[string]any{"Account": accountName})
		}
		return entity.GitHubAccount{}, false, svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
	}
	return account, true, ""
}

func (svc *SlashCommandService) repositoryAccessText(access providerrepo.RepositoryAccess) string {
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

type mattermostDialogState struct {
	View         string `json:"view"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	ChannelID    string `json:"channel_id"`
	PostID       string `json:"post_id"`
	UserName     string `json:"user_name"`
}

func encodeDialogState(command MenuActionCommand) string {
	state := mattermostDialogState{
		View:         normalizeMenuView(command.View),
		ResourceType: strings.TrimSpace(command.Resource),
		ResourceID:   strings.TrimSpace(command.ID),
		ChannelID:    strings.TrimSpace(command.ChannelID),
		PostID:       strings.TrimSpace(command.PostID),
		UserName:     strings.TrimSpace(command.UserName),
	}
	data, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeDialogState(raw string) (mattermostDialogState, error) {
	if strings.TrimSpace(raw) == "" {
		return mattermostDialogState{View: menuViewRepositories}, nil
	}
	var state mattermostDialogState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return mattermostDialogState{}, err
	}
	state.View = normalizeMenuView(defaultString(state.View, menuViewRepositories))
	state.ResourceType = strings.TrimSpace(state.ResourceType)
	state.ResourceID = strings.TrimSpace(state.ResourceID)
	state.ChannelID = strings.TrimSpace(state.ChannelID)
	state.PostID = strings.TrimSpace(state.PostID)
	state.UserName = strings.TrimSpace(state.UserName)
	return state, nil
}

func submissionString(submission map[string]any, key string) string {
	if submission == nil {
		return ""
	}
	value, ok := submission[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		text = fmt.Sprint(value)
	}
	return strings.TrimSpace(text)
}

func parseSubmittedRepository(value string) (string, string, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok || strings.Contains(name, "/") {
		return "", "", false
	}
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if !validIdentifier(owner) || !validIdentifier(name) {
		return "", "", false
	}
	return strings.ToLower(owner), strings.ToLower(name), true
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
		Provider:          provider,
		Owner:             strings.ToLower(owner),
		Name:              strings.ToLower(name),
		DefaultBranch:     branch,
		GitHubAccountName: "primary",
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
	identifierRE           = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	branchRE               = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
	promptTemplateIDRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	runtimeRunRE           = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,47}[a-z0-9])?$`)
	kubernetesSecretNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	githubUsernameRE       = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	accountEmailRE         = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	githubPRURLRE          = regexp.MustCompile(`/pull/([0-9]+)(?:$|[/?#])`)
)

const (
	menuViewMain         = "main"
	menuViewStartFlow    = "start_flow"
	menuViewPending      = "pending"
	menuViewRepositories = "repositories"
	menuViewAccounts     = "accounts"
	menuViewOpenAI       = "openai"
	menuViewGitHub       = "github"
	menuViewProfiles     = "profiles"
	menuViewPrompts      = "prompts"
	menuViewRuntime      = "runtime"
	menuViewSystem       = "system"
	menuViewHelp         = "help"

	menuDialogRepositoryAdd       = "repo_add"
	menuDialogRepositoryEdit      = "repo_edit"
	menuDialogRepositoryDelete    = "repo_delete"
	menuDialogRepositorySearch    = "repo_search"
	menuDialogOpenAIAuth          = "openai_auth"
	menuDialogOpenAIStatus        = "openai_status"
	menuDialogOpenAICleanup       = "openai_cleanup"
	menuDialogOpenAIDelete        = "openai_delete"
	menuDialogGitHubAccountAdd    = "github_account_add"
	menuDialogGitHubAccountEdit   = "github_account_edit"
	menuDialogGitHubAccountDelete = "github_account_delete"
	menuDialogProfileUpsert       = "profile_upsert"
	menuDialogPromptEdit          = "prompt_edit"
	menuDialogFlowStart           = "flow_start"
	menuDialogRuntimePruneApply   = "runtime_prune_apply"

	menuActionList               = "list"
	menuActionShow               = "show"
	menuActionConfirmDelete      = "confirm_delete"
	menuActionDelete             = "delete"
	menuActionCancel             = "cancel"
	menuActionRepositoryOnboard  = "repository_onboard"
	menuActionRepositoryRepos    = "repository_repos"
	menuActionRepositoryBranches = "repository_branches"
	menuActionRepositoryConnect  = "repository_connect"
	menuActionRepositoryCheck    = "repository_check"
	menuActionRepositoryWebhook  = "repository_webhook"
	menuActionOpenAIAuth         = "openai_auth"
	menuActionOpenAIStatus       = "openai_status"
	menuActionOpenAICleanup      = "openai_cleanup"
	menuActionSystemStatus       = "system_status"
	menuActionTokenCheck         = "token_check"
	menuActionLocaleGet          = "locale_get"
	menuActionLocaleSetRU        = "locale_set_ru"
	menuActionLocaleSetEN        = "locale_set_en"
	menuActionRuntimeSmoke       = "runtime_smoke"
	menuActionRuntimePruneDry    = "runtime_prune_dry"
	menuActionRuntimePruneApply  = "runtime_prune_apply"
	menuActionRuntimeCleanup     = "runtime_cleanup"
	menuActionPromptHelp         = "prompt_help"
	menuActionPromptRender       = "prompt_render"
	menuActionProfileEnable      = "profile_enable"
	menuActionProfileDisable     = "profile_disable"
	menuActionFlowAdvance        = "flow_advance"
	menuActionFlowCard           = "flow_card"
	menuActionFlowCleanup        = "flow_cleanup"
	menuActionFlowApprove        = "flow_approve"
	menuActionFlowReject         = "flow_reject"
	menuActionFlowRerun          = "flow_rerun"
	menuActionFlowStop           = "flow_stop"
	menuActionFlowHold           = "flow_hold"
	menuActionFlowResume         = "flow_resume"

	menuResourceRepository     = "repository"
	menuResourceOpenAIAccount  = "openai_account"
	menuResourceGitHubAccount  = "github_account"
	menuResourceProfile        = "profile"
	menuResourcePromptTemplate = "prompt_template"
	menuResourceFlow           = "flow"
	menuResourceRun            = "run"
	menuResourceSystem         = "system"
	menuResourceRuntime        = "runtime"

	dialogCallbackRepositoryAdd          = "agents_repo_add"
	dialogCallbackRepositoryEdit         = "agents_repo_edit"
	dialogCallbackRepositoryDelete       = "agents_repo_delete"
	dialogCallbackRepositorySearch       = "agents_repo_search"
	dialogCallbackRepositorySearchPick   = "agents_repo_search_pick"
	dialogCallbackRepositorySearchBranch = "agents_repo_search_branch"
	dialogCallbackOpenAIAuth             = "agents_openai_auth"
	dialogCallbackOpenAIStatus           = "agents_openai_status"
	dialogCallbackOpenAICleanup          = "agents_openai_cleanup"
	dialogCallbackOpenAIDelete           = "agents_openai_delete"
	dialogCallbackGitHubAccountAdd       = "agents_github_account_add"
	dialogCallbackGitHubAccountEdit      = "agents_github_account_edit"
	dialogCallbackGitHubAccountDelete    = "agents_github_account_delete"
	dialogCallbackProfileUpsert          = "agents_profile_upsert"
	dialogCallbackPromptEdit             = "agents_prompt_edit"
	dialogCallbackFlowStart              = "agents_flow_start"
	dialogCallbackRuntimePruneApply      = "agents_runtime_prune_apply"

	dialogFieldProvider         = "provider"
	dialogFieldRepository       = "repository"
	dialogFieldRepositoryChoice = "repository_choice"
	dialogFieldDefaultBranch    = "default_branch"
	dialogFieldBranchChoice     = "branch_choice"
	dialogFieldConfirm          = "confirm"
	dialogFieldAccount          = "account"
	dialogFieldProfile          = "profile"
	dialogFieldSearch           = "search"
	dialogFieldSecretRef        = "secret_ref"
	dialogFieldToken            = "token"
	dialogFieldUsername         = "username"
	dialogFieldEmail            = "email"
	dialogFieldStatus           = "status"
	dialogFieldRole             = "role"
	dialogFieldDescription      = "description"
	dialogFieldOpenAIAccount    = "openai_account"
	dialogFieldGitHubAccount    = "github_account"
	dialogFieldKubernetesAccess = "kubernetes_access"
	dialogFieldSandboxMode      = "sandbox_mode"
	dialogFieldConfigOverlay    = "config_overlay"
	dialogFieldTemplateBody     = "template_body"
	dialogFieldFlowRepository   = "flow_repository"
	dialogFieldDeveloperProfile = "developer_profile"
	dialogFieldReviewerProfile  = "reviewer_profile"
	dialogFieldFlowTitle        = "flow_title"
	dialogFieldFlowTask         = "flow_task"
	dialogFieldMaxAttempts      = "max_attempts"
	dialogFieldOlderThan        = "older_than"

	openAIDialogActionAuth    = "auth"
	openAIDialogActionStatus  = "status"
	openAIDialogActionCleanup = "cleanup"
	openAIDialogActionDelete  = "delete"

	defaultFlowMaxAttempts = 3

	flowStatusApprovedByReviewer = "approved_by_reviewer"
	flowStatusBlocked            = "blocked"
	flowStatusChangesRequested   = "changes_requested"
	flowStatusCleaned            = "cleaned"
	flowStatusCreated            = "created"
	flowStatusDeveloperFailed    = "developer_failed"
	flowStatusDeveloperRunning   = "developer_running"
	flowStatusFixRunning         = "fix_running"
	flowStatusHeld               = "held"
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

	entityListPageSize = 4
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

func validKubernetesSecretName(value string) bool {
	return kubernetesSecretNameRE.MatchString(value) && !strings.Contains(value, "..")
}

func validGitHubUsername(value string) bool {
	return githubUsernameRE.MatchString(value)
}

func validAccountEmail(value string) bool {
	return len(value) <= 150 && accountEmailRE.MatchString(value)
}

func validGitHubAccountStatus(value string) bool {
	switch value {
	case "configured", "disabled":
		return true
	default:
		return false
	}
}

func validProfileRole(value string) bool {
	switch value {
	case "developer", "reviewer", "deployer", "technical_reviewer", "lexical_guard", "manager":
		return true
	default:
		return false
	}
}

func validKubernetesAccess(value string) bool {
	switch value {
	case "read-only", "cluster-admin":
		return true
	default:
		return false
	}
}

func validSandboxMode(value string) bool {
	switch value {
	case "danger-full-access", "workspace-write", "read-only":
		return true
	default:
		return false
	}
}

func normalizeMenuView(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case menuViewProjects:
		return menuViewProjects
	case menuViewStartFlow:
		return menuViewStartFlow
	case menuViewPending:
		return menuViewPending
	case menuViewRepositories:
		return menuViewRepositories
	case menuViewAccounts:
		return menuViewAccounts
	case menuViewOpenAI:
		return menuViewOpenAI
	case menuViewGitHub:
		return menuViewGitHub
	case menuViewRoles:
		return menuViewRoles
	case menuViewChats:
		return menuViewChats
	case menuViewProfiles:
		return menuViewProfiles
	case menuViewPrompts:
		return menuViewPrompts
	case menuViewRuntime:
		return menuViewRuntime
	case menuViewSystem:
		return menuViewSystem
	case menuViewAdvanced:
		return menuViewAdvanced
	case menuViewHelp:
		return menuViewHelp
	default:
		return menuViewMain
	}
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

func parseRuntimePruneArgs(args []string) (time.Duration, bool, error) {
	olderThan := 24 * time.Hour
	dryRun := true
	durationSet := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--apply":
			dryRun = false
		default:
			if durationSet {
				return 0, true, fmt.Errorf("only one duration is allowed")
			}
			parsed, err := time.ParseDuration(arg)
			if err != nil || parsed <= 0 {
				return 0, true, fmt.Errorf("invalid retention duration")
			}
			olderThan = parsed
			durationSet = true
		}
	}
	return olderThan, dryRun, nil
}

func formatRunIDList(runIDs []string, limit int) string {
	if len(runIDs) == 0 {
		return "-"
	}
	if limit <= 0 || len(runIDs) <= limit {
		return strings.Join(runIDs, ", ")
	}
	return strings.Join(runIDs[:limit], ", ") + fmt.Sprintf(" ... +%d", len(runIDs)-limit)
}

func flowOwnerTerminal(status string) bool {
	switch status {
	case flowStatusOwnerApproved, flowStatusOwnerRejected, flowStatusStopped, flowStatusCleaned:
		return true
	default:
		return false
	}
}

func flowNeedsOwnerAttention(flow entity.AgentFlow) bool {
	switch flow.Status {
	case flowStatusApprovedByReviewer, flowStatusWaitingOwner, flowStatusBlocked, flowStatusDeveloperFailed, flowStatusReviewerFailed, flowStatusHeld:
		return true
	default:
		return false
	}
}

func flowCanHold(flow entity.AgentFlow) bool {
	if flowOwnerTerminal(flow.Status) {
		return false
	}
	switch flow.Status {
	case flowStatusApprovedByReviewer, flowStatusWaitingOwner, flowStatusBlocked, flowStatusDeveloperFailed, flowStatusReviewerFailed:
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
	case flowStatusHeld:
		return "#f08c00"
	default:
		return "#868e96"
	}
}

func newFlowID(repoName string) string {
	suffix := strconv.FormatInt(time.Now().UTC().Unix(), 36)
	var raw [3]byte
	if _, err := rand.Read(raw[:]); err == nil {
		suffix += "-" + fmt.Sprintf("%x", raw[:])
	}
	prefix := strings.ToLower(strings.TrimSpace(repoName))
	var builder strings.Builder
	lastDash := false
	for _, r := range prefix {
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
	prefix = strings.Trim(builder.String(), "-")
	if prefix == "" {
		prefix = "flow"
	}
	maxPrefix := 44 - len(suffix) - 1
	if maxPrefix < 1 {
		return "flow-" + suffix
	}
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-")
	}
	if prefix == "" {
		prefix = "flow"
	}
	return prefix + "-" + suffix
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

func repositoryResourceID(repo entity.Repository) string {
	return repo.Provider + ":" + repo.FullName()
}

type repositoryOnboardingState struct {
	ProjectID     int64  `json:"project_id"`
	Account       string `json:"account"`
	Provider      string `json:"provider"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Branch        string `json:"branch"`
}

func repositoryOnboardingResourceID(state repositoryOnboardingState) string {
	data, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func parseRepositoryOnboardingResourceID(value string) (repositoryOnboardingState, bool) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return repositoryOnboardingState{}, false
	}
	var state repositoryOnboardingState
	if err := json.Unmarshal(data, &state); err != nil {
		return repositoryOnboardingState{}, false
	}
	state.Account = strings.TrimSpace(state.Account)
	state.Provider = strings.ToLower(defaultString(state.Provider, "github"))
	state.Owner = strings.TrimSpace(state.Owner)
	state.Name = strings.TrimSpace(state.Name)
	state.FullName = strings.TrimSpace(state.FullName)
	if state.FullName == "" && state.Owner != "" && state.Name != "" {
		state.FullName = state.Owner + "/" + state.Name
	}
	state.DefaultBranch = strings.TrimSpace(state.DefaultBranch)
	state.Branch = strings.TrimSpace(state.Branch)
	if state.Provider != "github" || state.Account == "" || state.Owner == "" || state.Name == "" {
		return repositoryOnboardingState{}, false
	}
	return state, true
}

func (svc *SlashCommandService) repositorySearchAccountName(ctx context.Context, state mattermostDialogState) (string, string) {
	if state.ResourceType != menuResourceProject {
		return strings.TrimSpace(state.ResourceID), ""
	}
	projectID, ok := parseInt64ID(state.ResourceID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return "", svc.t("menu.entity.invalid", nil)
	}
	project, err := svc.cfg.Store.GetProject(ctx, projectID)
	if err != nil {
		return "", svc.t("project.get.failed", map[string]any{"Error": safeError(err)})
	}
	if strings.TrimSpace(project.GitHubAccountName) == "" {
		return "", svc.t("project.github_account.required", map[string]any{"Project": project.Name})
	}
	return strings.TrimSpace(project.GitHubAccountName), ""
}

func parseRepositoryResourceID(value string) (string, string, string, bool) {
	provider, fullName, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || strings.TrimSpace(provider) == "" {
		return "", "", "", false
	}
	owner, name, ok := parseSubmittedRepository(fullName)
	if !ok {
		return "", "", "", false
	}
	return strings.ToLower(provider), owner, name, true
}

func gitHubAccountRef(account entity.GitHubAccount) providerrepo.GitHubAccountRef {
	return providerrepo.GitHubAccountRef{
		Name:      account.Name,
		SecretRef: account.SecretRef,
	}
}

func promptTemplateResourceID(profileName string, templateKey string) string {
	return profileName + "/" + templateKey
}

func parsePromptTemplateResourceID(value string) (string, string, bool) {
	profileName, templateKey, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok || strings.Contains(templateKey, "/") {
		return "", "", false
	}
	profileName = strings.ToLower(strings.TrimSpace(profileName))
	templateKey = strings.ToLower(strings.TrimSpace(templateKey))
	if !validPromptTemplateID(profileName) || !validPromptTemplateID(templateKey) {
		return "", "", false
	}
	return profileName, templateKey, true
}

func entityPageBounds(total int, page int) (int, int, int) {
	if total <= 0 {
		return 0, 0, 0
	}
	pages := (total + entityListPageSize - 1) / entityListPageSize
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * entityListPageSize
	end := start + entityListPageSize
	if end > total {
		end = total
	}
	return start, end, page
}

func applyMenuCardIdentity(card *MattermostCard, command MenuActionCommand) {
	if card == nil {
		return
	}
	card.ChannelID = strings.TrimSpace(command.ChannelID)
	card.PostID = strings.TrimSpace(command.PostID)
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

func (svc *SlashCommandService) recordProfileAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "profile",
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

func (svc *SlashCommandService) openAIAccountProfileRefs(ctx context.Context, name string) ([]string, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, nil
	}
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0)
	for _, profile := range profiles {
		if defaultString(profile.OpenAIAccountName, "primary") == name {
			refs = append(refs, profile.Name)
		}
	}
	sort.Strings(refs)
	return refs, nil
}

func (svc *SlashCommandService) githubAccountProfileRefs(ctx context.Context, name string) ([]string, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, nil
	}
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0)
	for _, profile := range profiles {
		if defaultString(profile.GitHubAccountName, "primary") == name {
			refs = append(refs, profile.Name)
		}
	}
	sort.Strings(refs)
	return refs, nil
}

func (svc *SlashCommandService) githubAccountRepositoryRefs(ctx context.Context, name string) ([]string, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, nil
	}
	repositories, err := svc.cfg.Store.ListRepositories(ctx, 100)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0)
	for _, repo := range repositories {
		if repo.Provider == "github" && defaultString(repo.GitHubAccountName, "primary") == name {
			refs = append(refs, repo.FullName())
		}
	}
	sort.Strings(refs)
	return refs, nil
}

func (svc *SlashCommandService) githubAccountProjectRefs(ctx context.Context, name string) ([]string, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, nil
	}
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0)
	for _, project := range projects {
		if strings.TrimSpace(project.GitHubAccountName) == name {
			refs = append(refs, project.Name)
		}
	}
	sort.Strings(refs)
	return refs, nil
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

func (svc *SlashCommandService) openAIAccountOptions(ctx context.Context, selected string) ([]MattermostDialogOption, string) {
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		return nil, svc.t("openai.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(accounts) == 0 {
		return nil, svc.t("openai.list.empty", nil)
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	options := make([]MattermostDialogOption, 0, len(accounts))
	for _, account := range accounts {
		text := svc.t("dialog.account.option", map[string]any{"Account": account.Name, "Status": account.Status})
		options = append(options, MattermostDialogOption{Text: text, Value: account.Name})
	}
	return ensureDialogOption(options, selected), ""
}

func (svc *SlashCommandService) githubAccountOptions(ctx context.Context, selected string) ([]MattermostDialogOption, string) {
	accounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		return nil, svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(accounts) == 0 {
		return nil, svc.t("github.account.list.empty", nil)
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	options := make([]MattermostDialogOption, 0, len(accounts))
	for _, account := range accounts {
		text := svc.t("dialog.account.option", map[string]any{"Account": account.Name, "Status": account.Status})
		options = append(options, MattermostDialogOption{Text: text, Value: account.Name})
	}
	return ensureDialogOption(options, selected), ""
}

func (svc *SlashCommandService) repositoryOptions(ctx context.Context) ([]MattermostDialogOption, string) {
	repositories, err := svc.cfg.Store.ListRepositories(ctx, 100)
	if err != nil {
		return nil, svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})
	}
	if len(repositories) == 0 {
		return nil, svc.t("repo.list.empty", nil)
	}
	sort.Slice(repositories, func(i int, j int) bool { return repositories[i].FullName() < repositories[j].FullName() })
	options := make([]MattermostDialogOption, 0, len(repositories))
	for _, repo := range repositories {
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.flow.repository.option", map[string]any{"Repository": repo.FullName(), "Branch": repo.DefaultBranch}),
			Value: repositoryResourceID(repo),
		})
	}
	return options, ""
}

func (svc *SlashCommandService) enabledProfileOptions(ctx context.Context) ([]MattermostDialogOption, string) {
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		return nil, svc.t("profile.list.read_failed", map[string]any{"Error": safeError(err)})
	}
	sort.Slice(profiles, func(i int, j int) bool { return profiles[i].Name < profiles[j].Name })
	options := make([]MattermostDialogOption, 0, len(profiles))
	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.flow.profile.option", map[string]any{"Profile": profile.Name, "Role": profile.Role}),
			Value: profile.Name,
		})
	}
	if len(options) == 0 {
		return nil, svc.t("profile.list.empty", nil)
	}
	return options, ""
}

func (svc *SlashCommandService) ensurePromptTemplatesForProfile(ctx context.Context, profile entity.AgentProfile) (int, error) {
	seeds := promptSeedsForRole(profile.Role)
	created := 0
	for _, seed := range seeds {
		if _, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, profile.Name, seed.TemplateKey); err == nil {
			continue
		}
		body, err := svc.promptTemplateSeedBody(ctx, seed)
		if err != nil {
			return created, err
		}
		if _, newTemplate, err := svc.cfg.Store.UpsertAgentPromptTemplate(ctx, adminrepo.UpsertAgentPromptTemplateInput{
			ProfileName: profile.Name,
			TemplateKey: seed.TemplateKey,
			Body:        body,
		}); err != nil {
			return created, err
		} else if newTemplate {
			created++
		}
	}
	return created, nil
}

func (svc *SlashCommandService) promptTemplateSeedBody(ctx context.Context, seed promptTemplateSeed) (string, error) {
	item, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, seed.SourceProfile, seed.TemplateKey)
	if err == nil {
		return item.Body, nil
	}
	body := defaultPromptSeedBody(seed.TemplateKey)
	if strings.TrimSpace(body) == "" {
		return "", err
	}
	return body, nil
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

func runtimeDerivedStatus(current string, status runtimerepo.RunStatus) string {
	if status.JobFailed > 0 {
		return "failed"
	}
	if status.JobSucceeded > 0 {
		return "succeeded"
	}
	if status.JobActive > 0 {
		return "running"
	}
	return defaultString(current, "pending")
}

func (svc *SlashCommandService) runtimeStatusBrief(status runtimerepo.RunStatus) string {
	lines := []string{
		svc.t("runtime.status.identity", map[string]any{"RunID": status.RunID, "Namespace": status.Namespace, "Job": status.JobName, "PVC": status.PVCName}),
		svc.t("runtime.status.job", map[string]any{"Active": status.JobActive, "Succeeded": status.JobSucceeded, "Failed": status.JobFailed}),
		svc.t("runtime.status.pod", map[string]any{"Pod": emptyAsUnknown(status.PodName), "Phase": emptyAsUnknown(status.PodPhase)}),
	}
	return strings.Join(lines, "\n")
}

func flowPRValue(flow entity.AgentFlow) string {
	if flow.PRNumber <= 0 && strings.TrimSpace(flow.PRURL) == "" {
		return "`-`"
	}
	if flow.PRNumber > 0 && strings.TrimSpace(flow.PRURL) != "" {
		return fmt.Sprintf("`#%d` %s", flow.PRNumber, flow.PRURL)
	}
	if flow.PRNumber > 0 {
		return fmt.Sprintf("`#%d`", flow.PRNumber)
	}
	return strings.TrimSpace(flow.PRURL)
}

type promptTemplateSeed struct {
	SourceProfile string
	TemplateKey   string
}

func promptSeedsForRole(role string) []promptTemplateSeed {
	switch role {
	case "developer", "deployer":
		return []promptTemplateSeed{
			{SourceProfile: "developer", TemplateKey: developerImplementTaskKey},
			{SourceProfile: "developer", TemplateKey: developerFixReviewKey},
		}
	case "reviewer", "technical_reviewer", "lexical_guard":
		return []promptTemplateSeed{{SourceProfile: "reviewer", TemplateKey: reviewPRTemplateKey}}
	default:
		return nil
	}
}

func defaultPromptSeedBody(templateKey string) string {
	switch templateKey {
	case developerImplementTaskKey:
		return "You are the matter-codex developer agent.\n\nLanguage: {{.Locale.Language}}.\nRepository: {{.Repository.FullName}}\nTask: {{.Task.Body}}\n"
	case developerFixReviewKey:
		return "You are the matter-codex developer agent fixing review feedback.\n\nLanguage: {{.Locale.Language}}.\nRepository: {{.Repository.FullName}}\nPull request: #{{.PullRequest.Number}} {{.PullRequest.URL}}\nTask: {{.Task.Body}}\n"
	case reviewPRTemplateKey:
		return "You are the matter-codex reviewer agent.\n\nLanguage: {{.Locale.Language}}.\nRepository: {{.Repository.FullName}}\nPull request: #{{.PullRequest.Number}} {{.PullRequest.URL}}\n"
	default:
		return ""
	}
}

func profileRoleOptions() []MattermostDialogOption {
	return []MattermostDialogOption{
		{Text: "developer", Value: "developer"},
		{Text: "reviewer", Value: "reviewer"},
		{Text: "deployer", Value: "deployer"},
		{Text: "technical_reviewer", Value: "technical_reviewer"},
		{Text: "lexical_guard", Value: "lexical_guard"},
		{Text: "manager", Value: "manager"},
	}
}

func ensureDialogOption(options []MattermostDialogOption, selected string) []MattermostDialogOption {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return options
	}
	for _, option := range options {
		if option.Value == selected {
			return options
		}
	}
	return append([]MattermostDialogOption{{Text: selected, Value: selected}}, options...)
}

func openAICredentialName(accountName string) string {
	return "openai:" + accountName
}

func githubCredentialName(accountName string) string {
	return "github:" + accountName
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

func githubAccountSecretName(baseName string, accountName string) string {
	baseName = strings.Trim(strings.TrimSpace(baseName), "-")
	if baseName == "" {
		baseName = "matter-codex-github"
	}
	accountName = strings.Trim(strings.TrimSpace(accountName), "-")
	if accountName == "" || accountName == "primary" {
		return baseName
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
