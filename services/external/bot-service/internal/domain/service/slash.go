package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
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
	StatusService         *StatusService
	Store                 adminrepo.Repository
	ChannelManager        MattermostChannelManager
	RepositoryProvider    providerrepo.RepositoryProvider
	DefaultTeamName       string
	BotTokenConfigured    bool
	SlashTokenConfigured  bool
	GitHubTokenConfigured bool
	DatabaseConfigured    bool
	StorageReady          bool
	MattermostConfigured  bool
	ChannelManagerEnabled bool
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
	case "profile":
		return svc.handleProfile(ctx, fields[1:])
	case "github":
		return svc.handleGitHub(ctx, fields[1:], command)
	default:
		return "matter-codex: неизвестная команда. Доступно: `/agents help`."
	}
}

func (svc *SlashCommandService) handleRepo(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return "matter-codex: доступно `/agents repo add github owner/name [default-branch]` и `/agents repo list`."
	}
	switch args[0] {
	case "add":
		return svc.handleRepoAdd(ctx, args[1:], command)
	case "list":
		return svc.handleRepoList(ctx)
	default:
		return "matter-codex: неизвестная repo-команда. Доступно: `/agents repo add` и `/agents repo list`."
	}
}

func (svc *SlashCommandService) handleRepoAdd(ctx context.Context, args []string, command SlashCommand) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return "matter-codex: storage ещё не готов, repo add недоступен."
	}
	input, err := parseRepoAdd(args)
	if err != nil {
		return "matter-codex: " + err.Error()
	}
	channelName := repositoryChannelName(input.Owner, input.Name)
	if svc.cfg.ChannelManager != nil {
		if _, err := svc.cfg.ChannelManager.EnsureRepositoryChannel(ctx, svc.cfg.DefaultTeamName, channelName, "repo "+input.Owner+"/"+input.Name); err != nil {
			return "matter-codex: repo channel не создан: " + safeError(err)
		}
	}
	input.MattermostChannel = channelName
	repo, created, err := svc.cfg.Store.UpsertRepository(ctx, input)
	if err != nil {
		return "matter-codex: repository не сохранён: " + safeError(err)
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "repository.upserted",
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "repository",
		ResourceName: repo.Provider + ":" + repo.FullName(),
		Summary:      "repository metadata upserted from Mattermost slash command",
	})
	state := "обновлён"
	if created {
		state = "добавлен"
	}
	return fmt.Sprintf("matter-codex: repository %s `%s:%s`.\nchannel: `%s`\ndefault branch: `%s`", state, repo.Provider, repo.FullName(), repo.MattermostChannel, repo.DefaultBranch)
}

func (svc *SlashCommandService) handleRepoList(ctx context.Context) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return "matter-codex: storage ещё не готов, repo list недоступен."
	}
	repositories, err := svc.cfg.Store.ListRepositories(ctx, 20)
	if err != nil {
		return "matter-codex: repositories не прочитаны: " + safeError(err)
	}
	if len(repositories) == 0 {
		return "matter-codex: repositories пока не добавлены."
	}
	lines := []string{"matter-codex repositories:"}
	for _, repo := range repositories {
		lines = append(lines, fmt.Sprintf("- `%s:%s` branch `%s` channel `%s` status `%s`", repo.Provider, repo.FullName(), repo.DefaultBranch, repo.MattermostChannel, repo.Status))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleToken(args []string) string {
	if len(args) != 1 || args[0] != "check" {
		return "matter-codex: доступно `/agents token check`."
	}
	lines := []string{
		"matter-codex token check:",
		"- mattermost bot token: " + configuredLabel(svc.cfg.BotTokenConfigured),
		"- mattermost slash token: " + configuredLabel(svc.cfg.SlashTokenConfigured),
		"- github token: " + configuredLabel(svc.cfg.GitHubTokenConfigured),
		"- database dsn: " + configuredLabel(svc.cfg.DatabaseConfigured),
		"- storage: " + readyLabel(svc.cfg.StorageReady),
		"- mattermost channel manager: " + configuredLabel(svc.cfg.ChannelManagerEnabled),
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleProfile(ctx context.Context, args []string) string {
	if len(args) != 1 || args[0] != "list" {
		return "matter-codex: доступно `/agents profile list`."
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return "matter-codex: storage ещё не готов, profile list недоступен."
	}
	profiles, err := svc.cfg.Store.ListAgentProfiles(ctx)
	if err != nil {
		return "matter-codex: agent profiles не прочитаны: " + safeError(err)
	}
	if len(profiles) == 0 {
		return "matter-codex: agent profiles пока не заведены."
	}
	lines := []string{"matter-codex agent profiles:"}
	for _, profile := range profiles {
		enabled := "disabled"
		if profile.Enabled {
			enabled = "enabled"
		}
		lines = append(lines, fmt.Sprintf("- `%s` role `%s` %s - %s", profile.Name, profile.Role, enabled, profile.Description))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) helpText() string {
	commands := []string{
		"`/agents status` - показать runtime status",
		"`/agents repo add github owner/name [default-branch]` - добавить repository metadata и repo-channel",
		"`/agents repo list` - показать подключённые repositories",
		"`/agents github check owner/name` - проверить GitHub repo access",
		"`/agents github branch dry-run owner/name branch [base]` - проверить создание branch без изменений",
		"`/agents github branch create owner/name branch [base]` - создать branch от base",
		"`/agents github pr dry-run owner/name head base title...` - проверить параметры PR без создания",
		"`/agents github pr status owner/name number` - показать PR status/reviews/comments",
		"`/agents token check` - показать безопасный статус токенов и storage",
		"`/agents profile list` - показать seed agent profiles",
	}
	return "matter-codex commands:\n" + strings.Join(commands, "\n")
}

func (svc *SlashCommandService) handleGitHub(ctx context.Context, args []string, command SlashCommand) string {
	if svc.cfg.RepositoryProvider == nil {
		return "matter-codex: GitHub provider не настроен, задайте GitHub token в Kubernetes Secret."
	}
	if len(args) == 0 {
		return "matter-codex: доступно `/agents github check`, `/agents github branch`, `/agents github pr`."
	}
	switch args[0] {
	case "check":
		return svc.handleGitHubCheck(ctx, args[1:], command)
	case "branch":
		return svc.handleGitHubBranch(ctx, args[1:], command)
	case "pr":
		return svc.handleGitHubPR(ctx, args[1:], command)
	default:
		return "matter-codex: неизвестная github-команда. Доступно: `check`, `branch`, `pr`."
	}
}

func (svc *SlashCommandService) handleGitHubCheck(ctx context.Context, args []string, command SlashCommand) string {
	ref, err := parseRepositoryRef(args, "github check")
	if err != nil {
		return "matter-codex: " + err.Error()
	}
	access, err := svc.cfg.RepositoryProvider.CheckRepository(ctx, ref.Owner, ref.Name)
	if err != nil {
		return "matter-codex: GitHub repo access не проверен: " + safeError(err)
	}
	svc.recordGitHubAudit(ctx, command, "github.repository.checked", access.Owner+"/"+access.Name, "github repository access checked")
	return fmt.Sprintf("matter-codex GitHub repo access:\n- repo: `%s:%s/%s`\n- default branch: `%s`\n- private: `%t`\n- permissions: pull `%t`, push `%t`, maintain `%t`, admin `%t`",
		access.Provider,
		access.Owner,
		access.Name,
		access.DefaultBranch,
		access.Private,
		access.CanPull,
		access.CanPush,
		access.CanMaintain,
		access.CanAdmin,
	)
}

func (svc *SlashCommandService) handleGitHubBranch(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) < 3 {
		return "matter-codex: нужен формат `/agents github branch dry-run owner/name branch [base]` или `/agents github branch create owner/name branch [base]`."
	}
	mode := args[0]
	if mode != "dry-run" && mode != "create" {
		return "matter-codex: github branch поддерживает только `dry-run` и `create`."
	}
	ref, err := parseRepositoryRef(args[1:2], "github branch")
	if err != nil {
		return "matter-codex: " + err.Error()
	}
	branch := args[2]
	base := "main"
	if len(args) >= 4 {
		base = args[3]
	}
	if !validBranch(branch) || !validBranch(base) {
		return "matter-codex: branch содержит недопустимые символы."
	}
	if mode == "dry-run" {
		baseRef, err := svc.cfg.RepositoryProvider.ResolveBranch(ctx, ref.Owner, ref.Name, base)
		if err != nil {
			return "matter-codex: GitHub branch dry-run не выполнен: " + safeError(err)
		}
		return fmt.Sprintf("matter-codex GitHub branch dry-run:\n- repo: `github:%s/%s`\n- branch: `%s`\n- base: `%s`\n- base sha: `%s`\n- changes: none", ref.Owner, ref.Name, branch, base, shortSHA(baseRef.SHA))
	}
	created, err := svc.cfg.RepositoryProvider.CreateBranch(ctx, ref.Owner, ref.Name, branch, base)
	if err != nil {
		return "matter-codex: GitHub branch не создан: " + safeError(err)
	}
	svc.recordGitHubAudit(ctx, command, "github.branch.created", ref.Owner+"/"+ref.Name+":"+branch, "github branch created from Mattermost slash command")
	return fmt.Sprintf("matter-codex GitHub branch создан:\n- repo: `github:%s/%s`\n- branch: `%s`\n- sha: `%s`", created.Owner, created.Name, created.Branch, shortSHA(created.SHA))
}

func (svc *SlashCommandService) handleGitHubPR(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 {
		return "matter-codex: доступно `/agents github pr dry-run owner/name head base title...` и `/agents github pr status owner/name number`."
	}
	switch args[0] {
	case "dry-run":
		return svc.handleGitHubPRDryRun(ctx, args[1:])
	case "create":
		return svc.handleGitHubPRCreate(ctx, args[1:], command)
	case "status":
		return svc.handleGitHubPRStatus(ctx, args[1:])
	default:
		return "matter-codex: github pr поддерживает `dry-run`, `create` и `status`."
	}
}

func (svc *SlashCommandService) handleGitHubPRDryRun(ctx context.Context, args []string) string {
	input, err := parsePullRequestInput(args)
	if err != nil {
		return "matter-codex: " + err.Error()
	}
	preview, err := svc.cfg.RepositoryProvider.PreviewPullRequest(ctx, input)
	if err != nil {
		return "matter-codex: GitHub PR dry-run не выполнен: " + safeError(err)
	}
	return fmt.Sprintf("matter-codex GitHub PR dry-run:\n- repo: `github:%s/%s`\n- head: `%s` `%s`\n- base: `%s` `%s`\n- title: `%s`\n- changes: none", preview.Owner, preview.Name, preview.Head, shortSHA(preview.HeadSHA), preview.Base, shortSHA(preview.BaseSHA), preview.Title)
}

func (svc *SlashCommandService) handleGitHubPRCreate(ctx context.Context, args []string, command SlashCommand) string {
	input, err := parsePullRequestInput(args)
	if err != nil {
		return "matter-codex: " + err.Error()
	}
	input.Draft = true
	input.Body = "Created by matter-codex GitHub adapter smoke command."
	summary, err := svc.cfg.RepositoryProvider.CreatePullRequest(ctx, input)
	if err != nil {
		return "matter-codex: GitHub PR не создан: " + safeError(err)
	}
	svc.recordGitHubAudit(ctx, command, "github.pull_request.created", input.Owner+"/"+input.Name+"#"+strconv.Itoa(summary.Number), "github draft pull request created from Mattermost slash command")
	return fmt.Sprintf("matter-codex GitHub draft PR создан:\n- repo: `github:%s/%s`\n- PR: `#%d` `%s`\n- state: `%s`\n- url: %s", summary.Owner, summary.Name, summary.Number, summary.Title, summary.State, summary.URL)
}

func (svc *SlashCommandService) handleGitHubPRStatus(ctx context.Context, args []string) string {
	if len(args) != 2 {
		return "matter-codex: нужен формат `/agents github pr status owner/name number`."
	}
	ref, err := parseRepositoryRef(args[:1], "github pr status")
	if err != nil {
		return "matter-codex: " + err.Error()
	}
	number, err := strconv.Atoi(args[1])
	if err != nil || number <= 0 {
		return "matter-codex: PR number должен быть положительным числом."
	}
	summary, err := svc.cfg.RepositoryProvider.GetPullRequest(ctx, ref.Owner, ref.Name, number)
	if err != nil {
		return "matter-codex: GitHub PR status не прочитан: " + safeError(err)
	}
	lines := []string{
		"matter-codex GitHub PR status:",
		fmt.Sprintf("- repo: `github:%s/%s`", summary.Owner, summary.Name),
		fmt.Sprintf("- PR: `#%d` `%s`", summary.Number, summary.Title),
		fmt.Sprintf("- state: `%s`, draft `%t`, merged `%t`, mergeable `%s`", summary.State, summary.Draft, summary.Merged, emptyAsUnknown(summary.MergeableState)),
		fmt.Sprintf("- reviews fetched: `%d`, review comments fetched: `%d`", summary.ReviewCount, summary.ReviewCommentCount),
	}
	for _, review := range summary.LatestReviews {
		lines = append(lines, fmt.Sprintf("- review: `%s` by `%s`", review.State, emptyAsUnknown(review.Author)))
	}
	if summary.URL != "" {
		lines = append(lines, "- url: "+summary.URL)
	}
	return strings.Join(lines, "\n")
}

func parseRepoAdd(args []string) (adminrepo.UpsertRepositoryInput, error) {
	if len(args) == 0 {
		return adminrepo.UpsertRepositoryInput{}, fmt.Errorf("нужен repository: `/agents repo add github owner/name [default-branch]`")
	}
	provider := "github"
	repoArg := args[0]
	branch := "main"
	if isProvider(repoArg) {
		provider = repoArg
		if len(args) < 2 {
			return adminrepo.UpsertRepositoryInput{}, fmt.Errorf("нужен owner/name после provider")
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
		return adminrepo.UpsertRepositoryInput{}, fmt.Errorf("repository должен быть в формате owner/name")
	}
	if !validIdentifier(owner) || !validIdentifier(name) || !validBranch(branch) {
		return adminrepo.UpsertRepositoryInput{}, fmt.Errorf("repository или branch содержит недопустимые символы")
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
		return repositoryRef{}, fmt.Errorf("нужен repository: `/agents %s owner/name`", command)
	}
	owner, name, ok := strings.Cut(args[0], "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" {
		return repositoryRef{}, fmt.Errorf("repository должен быть в формате owner/name")
	}
	if !validIdentifier(owner) || !validIdentifier(name) {
		return repositoryRef{}, fmt.Errorf("repository содержит недопустимые символы")
	}
	return repositoryRef{
		Owner: strings.ToLower(owner),
		Name:  strings.ToLower(name),
	}, nil
}

func parsePullRequestInput(args []string) (providerrepo.PullRequestInput, error) {
	if len(args) < 4 {
		return providerrepo.PullRequestInput{}, fmt.Errorf("нужен формат `/agents github pr dry-run owner/name head base title...`")
	}
	ref, err := parseRepositoryRef(args[:1], "github pr")
	if err != nil {
		return providerrepo.PullRequestInput{}, err
	}
	head := args[1]
	base := args[2]
	title := strings.TrimSpace(strings.Join(args[3:], " "))
	if !validBranch(head) || !validBranch(base) {
		return providerrepo.PullRequestInput{}, fmt.Errorf("branch содержит недопустимые символы")
	}
	if title == "" {
		return providerrepo.PullRequestInput{}, fmt.Errorf("title не должен быть пустым")
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
