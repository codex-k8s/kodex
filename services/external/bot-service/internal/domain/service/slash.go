package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
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
	DefaultTeamName       string
	BotTokenConfigured    bool
	SlashTokenConfigured  bool
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
		"`/agents token check` - показать безопасный статус токенов и storage",
		"`/agents profile list` - показать seed agent profiles",
	}
	return "matter-codex commands:\n" + strings.Join(commands, "\n")
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
