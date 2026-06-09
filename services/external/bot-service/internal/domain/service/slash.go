package service

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

type MattermostChannelManager interface {
	EnsureRepositoryChannel(ctx context.Context, teamName string, channelName string, displayName string) (bool, error)
}

type SlashCommand struct {
	Text        string
	UserID      string
	UserName    string
	ChannelName string
	TeamDomain  string
}

type SlashCommandServiceConfig struct {
	Localizer               *texti18n.Localizer
	StatusService           *StatusService
	Store                   adminrepo.Repository
	ChannelManager          MattermostChannelManager
	RepositoryProvider      providerrepo.RepositoryProvider
	DefaultTeamName         string
	BotTokenConfigured      bool
	SlashTokenConfigured    bool
	GitHubTokenConfigured   bool
	GitHubWebhookConfigured bool
	DatabaseConfigured      bool
	StorageReady            bool
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
		return svc.handleToken(fields[1:])
	case "locale":
		return svc.handleLocale(fields[1:])
	case "profile":
		return svc.handleProfile(ctx, fields[1:])
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

func (svc *SlashCommandService) handleToken(args []string) string {
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
		svc.t("token.check.channel_manager", map[string]any{"Status": configuredLabel(svc.cfg.Localizer, svc.cfg.ChannelManagerEnabled)}),
	}
	return strings.Join(lines, "\n")
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
			"Description": profile.Description,
		}))
	}
	return strings.Join(lines, "\n")
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
		svc.t("help.profile_list", nil),
		svc.t("help.locale", nil),
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

func isProvider(value string) bool {
	switch value {
	case "github", "gitlab":
		return true
	default:
		return false
	}
}

var (
	identifierRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	branchRE     = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
)

func validIdentifier(value string) bool {
	return identifierRE.MatchString(value)
}

func validBranch(value string) bool {
	return branchRE.MatchString(value)
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
