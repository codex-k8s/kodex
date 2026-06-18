package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

const (
	chatRunStatusRunning   = "running"
	chatRunStatusSucceeded = "succeeded"
	chatRunStatusFailed    = "failed"

	chatRunModeChat      = "chat"
	chatRunModeDeveloper = "developer"
	chatRunModeReviewer  = "reviewer"
)

var githubPullURLRE = regexp.MustCompile(`https://github\.com/([^/\s]+)/([^/\s]+)/pull/([0-9]+)`)

type MattermostThreadPostInput struct {
	ChannelID  string
	RootPostID string
	Message    string
}

type MattermostPostRef struct {
	ChannelID string
	PostID    string
}

type MattermostThreadPublisher interface {
	PostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error)
}

type ChatPostCommand struct {
	ChannelID  string
	PostID     string
	RootPostID string
	UserID     string
	UserName   string
	Message    string
}

type ChatRunResult struct {
	Ignored bool
	RunID   string
	Mode    string
}

type ChatRunServiceConfig struct {
	Localizer       *texti18n.Localizer
	Store           adminrepo.Repository
	RuntimeRunner   runtimerepo.Runner
	ThreadPublisher MattermostThreadPublisher
	StorageReady    bool
	RuntimeReady    bool
	DisableMonitor  bool
	MonitorInterval time.Duration
	MonitorTimeout  time.Duration
}

type ChatRunService struct {
	cfg ChatRunServiceConfig
}

func NewChatRunService(cfg ChatRunServiceConfig) *ChatRunService {
	if cfg.MonitorInterval <= 0 {
		cfg.MonitorInterval = 15 * time.Second
	}
	if cfg.MonitorTimeout <= 0 {
		cfg.MonitorTimeout = 6 * time.Hour
	}
	return &ChatRunService{cfg: cfg}
}

func (svc *ChatRunService) HandleChatPost(ctx context.Context, command ChatPostCommand) ChatRunResult {
	command = normalizeChatPostCommand(command)
	if command.ChannelID == "" || command.PostID == "" || command.Message == "" {
		return ChatRunResult{Ignored: true}
	}
	if strings.HasPrefix(command.Message, "/") {
		return ChatRunResult{Ignored: true}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		svc.postThread(ctx, command, svc.t("chat.run.storage_not_ready", nil))
		return ChatRunResult{}
	}
	if !svc.cfg.RuntimeReady || svc.cfg.RuntimeRunner == nil {
		svc.postThread(ctx, command, svc.t("runtime.not_configured", nil))
		return ChatRunResult{}
	}
	chat, err := svc.cfg.Store.GetChatByMattermostChannelID(ctx, command.ChannelID)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return ChatRunResult{Ignored: true}
		}
		svc.postThread(ctx, command, svc.t("chat.run.chat_lookup_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	project, err := svc.cfg.Store.GetProject(ctx, chat.ProjectID)
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.project_lookup_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	roles, err := svc.chatRoles(ctx, chat.ID)
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.roles_lookup_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	if len(roles) == 0 {
		svc.postThread(ctx, command, svc.t("chat.run.roles_empty", nil))
		return ChatRunResult{}
	}
	repositories, err := svc.chatRepositories(ctx, chat)
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.repositories_lookup_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	prRef := extractGitHubPullRequest(command.Message)
	role := selectChatRole(roles, prRef.Number)
	mode := chatRunMode(role, repositories, prRef.Number)
	if (mode == chatRunModeDeveloper || mode == chatRunModeReviewer) && len(repositories) == 0 {
		svc.postThread(ctx, command, svc.t("chat.run.repository_required", map[string]any{"Role": role.Name}))
		return ChatRunResult{}
	}
	openAIAccount, ok := svc.openAIAccount(ctx, role)
	if !ok {
		svc.postThread(ctx, command, svc.t("chat.run.openai_required", map[string]any{"Role": role.Name}))
		return ChatRunResult{}
	}
	gitHubAccount, gitHubOK := svc.gitHubAccount(ctx, role, firstRepository(repositories))
	if mode != chatRunModeChat && !gitHubOK {
		svc.postThread(ctx, command, svc.t("chat.run.github_required", map[string]any{"Role": role.Name}))
		return ChatRunResult{}
	}
	prompt, err := BuildRolePrompt(RolePromptInput{
		Project:      project,
		Role:         role,
		Chat:         chat,
		Repositories: repositories,
		UserMessage:  command.Message,
		Locale:       svc.localeData(),
	})
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.prompt_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	runID := newChatRunID(chat.ID)
	started, err := svc.startRun(ctx, chatRunStartInput{
		RunID:         runID,
		Mode:          mode,
		Project:       project,
		Role:          role,
		Chat:          chat,
		Repositories:  repositories,
		PRNumber:      prRef.Number,
		OpenAIAccount: openAIAccount,
		GitHubAccount: gitHubAccount,
		Prompt:        prompt,
		UserMessage:   command.Message,
	})
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.start_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	if err := svc.recordRun(ctx, mode, started, chatRunRecordInput{
		Chat:         chat,
		Role:         role,
		Repositories: repositories,
		PRNumber:     prRef.Number,
		UserName:     command.UserName,
	}); err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.record_failed", map[string]any{"RunID": runID, "Error": safeError(err)}))
	}
	svc.postThread(ctx, command, svc.t("chat.run.started", map[string]any{
		"RunID": runID,
		"Mode":  mode,
		"Role":  role.Name,
		"Job":   started.JobName,
		"PVC":   started.PVCName,
	}))
	if !svc.cfg.DisableMonitor {
		go svc.monitorRun(ctx, chatRunMonitorInput{
			RunID:      runID,
			Mode:       mode,
			ChannelID:  command.ChannelID,
			RootPostID: commandRootPostID(command),
		})
	}
	return ChatRunResult{RunID: runID, Mode: mode}
}

type githubPullRef struct {
	Owner  string
	Name   string
	Number int
}

type chatRunStartInput struct {
	RunID         string
	Mode          string
	Project       entity.Project
	Role          entity.AgentRole
	Chat          entity.Chat
	Repositories  []entity.ProjectRepository
	PRNumber      int
	OpenAIAccount entity.OpenAIAccount
	GitHubAccount entity.GitHubAccount
	Prompt        string
	UserMessage   string
}

type chatRunRecordInput struct {
	Chat         entity.Chat
	Role         entity.AgentRole
	Repositories []entity.ProjectRepository
	PRNumber     int
	UserName     string
}

type chatRunMonitorInput struct {
	RunID      string
	Mode       string
	ChannelID  string
	RootPostID string
}

func (svc *ChatRunService) startRun(ctx context.Context, input chatRunStartInput) (runtimerepo.StartedRun, error) {
	repo := firstRepository(input.Repositories)
	switch input.Mode {
	case chatRunModeReviewer:
		return svc.cfg.RuntimeRunner.StartReviewRun(ctx, runtimerepo.ReviewRunInput{
			RunID:               input.RunID,
			Profile:             input.Role.Name,
			CodexAuthSecretName: input.OpenAIAccount.SecretRef,
			GitHubSecretName:    input.GitHubAccount.SecretRef,
			Provider:            repo.Provider,
			Owner:               repo.Owner,
			Name:                repo.Name,
			PRNumber:            input.PRNumber,
			Prompt:              input.Prompt,
			SandboxMode:         input.Role.SandboxMode,
			ConfigOverlay:       input.Role.ConfigOverlay,
		})
	case chatRunModeDeveloper:
		return svc.cfg.RuntimeRunner.StartDeveloperRun(ctx, runtimerepo.DeveloperRunInput{
			RunID:               input.RunID,
			Profile:             input.Role.Name,
			CodexAuthSecretName: input.OpenAIAccount.SecretRef,
			GitHubSecretName:    input.GitHubAccount.SecretRef,
			Provider:            repo.Provider,
			Owner:               repo.Owner,
			Name:                repo.Name,
			BaseBranch:          defaultString(repo.DefaultBranch, "main"),
			HeadBranch:          "matter-codex-" + input.RunID,
			Title:               chatRunTitle(input.UserMessage),
			Task:                input.UserMessage,
			Prompt:              input.Prompt,
			SandboxMode:         input.Role.SandboxMode,
			ConfigOverlay:       input.Role.ConfigOverlay,
		})
	default:
		gitHubSecret := ""
		if input.GitHubAccount.SecretRef != "" {
			gitHubSecret = input.GitHubAccount.SecretRef
		}
		return svc.cfg.RuntimeRunner.StartChatRun(ctx, runtimerepo.ChatRunInput{
			RunID:               input.RunID,
			Profile:             input.Role.Name,
			CodexAuthSecretName: input.OpenAIAccount.SecretRef,
			GitHubSecretName:    gitHubSecret,
			Prompt:              input.Prompt,
			SandboxMode:         input.Role.SandboxMode,
			ConfigOverlay:       input.Role.ConfigOverlay,
		})
	}
}

func (svc *ChatRunService) recordRun(ctx context.Context, mode string, started runtimerepo.StartedRun, input chatRunRecordInput) error {
	repo := firstRepository(input.Repositories)
	_, err := svc.cfg.Store.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID:               started.RunID,
		FlowID:              "chat-" + strconv.FormatInt(input.Chat.ID, 10),
		ProfileName:         input.Role.Name,
		Role:                input.Role.RoleType,
		Provider:            repo.Provider,
		Owner:               repo.Owner,
		Name:                repo.Name,
		BaseBranch:          defaultString(repo.DefaultBranch, "main"),
		HeadBranch:          "matter-codex-" + started.RunID,
		Status:              chatRunStatusRunning,
		KubernetesNamespace: started.Namespace,
		JobName:             started.JobName,
		PVCName:             started.PVCName,
		Summary:             fmt.Sprintf("chat run mode=%s chat=%d role=%s owner=%s pr=%d", mode, input.Chat.ID, input.Role.Name, input.UserName, input.PRNumber),
	})
	return err
}

func (svc *ChatRunService) monitorRun(ctx context.Context, input chatRunMonitorInput) {
	deadline := time.NewTimer(svc.cfg.MonitorTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(svc.cfg.MonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			svc.postThreadByID(ctx, input.ChannelID, input.RootPostID, svc.t("chat.run.monitor_timeout", map[string]any{"RunID": input.RunID}))
			return
		case <-ticker.C:
			status, err := svc.cfg.RuntimeRunner.GetRunStatus(ctx, input.RunID)
			if err != nil {
				svc.postThreadByID(ctx, input.ChannelID, input.RootPostID, svc.t("chat.run.status_failed", map[string]any{"RunID": input.RunID, "Error": safeError(err)}))
				return
			}
			if status.JobSucceeded > 0 {
				_ = svc.updateRunArtifacts(ctx, input.RunID, chatRunStatusSucceeded, status)
				svc.postThreadByID(ctx, input.ChannelID, input.RootPostID, svc.runSuccessText(input, status))
				return
			}
			if status.JobFailed > 0 {
				_ = svc.updateRunArtifacts(ctx, input.RunID, chatRunStatusFailed, status)
				svc.postThreadByID(ctx, input.ChannelID, input.RootPostID, svc.runFailedText(input, status))
				return
			}
		}
	}
}

func (svc *ChatRunService) updateRunArtifacts(ctx context.Context, runID string, status string, runStatus runtimerepo.RunStatus) error {
	_, err := svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{
		RunID:  runID,
		Status: status,
		PRURL:  runStatus.Artifacts["pr-url"],
	})
	return err
}

func (svc *ChatRunService) runSuccessText(input chatRunMonitorInput, status runtimerepo.RunStatus) string {
	final := finalAnswerFromLog(status.LogTail)
	if final == "" {
		final = svc.t("chat.run.final_empty", nil)
	}
	return svc.t("chat.run.succeeded", map[string]any{
		"RunID":     input.RunID,
		"Mode":      input.Mode,
		"Artifacts": formatRunArtifacts(status.Artifacts),
		"Final":     final,
	})
}

func (svc *ChatRunService) runFailedText(input chatRunMonitorInput, status runtimerepo.RunStatus) string {
	return svc.t("chat.run.failed", map[string]any{
		"RunID": input.RunID,
		"Mode":  input.Mode,
		"Log":   truncateMattermostText(status.LogTail, 3000),
	})
}

func (svc *ChatRunService) chatRoles(ctx context.Context, chatID int64) ([]entity.AgentRole, error) {
	participants, err := svc.cfg.Store.ListChatParticipants(ctx, chatID)
	if err != nil {
		return nil, err
	}
	roles := make([]entity.AgentRole, 0, len(participants))
	for _, participant := range participants {
		if !participant.Enabled {
			continue
		}
		role, err := svc.cfg.Store.GetAgentRole(ctx, participant.RoleID)
		if err != nil {
			return nil, err
		}
		if role.Enabled {
			roles = append(roles, role)
		}
	}
	return roles, nil
}

func (svc *ChatRunService) chatRepositories(ctx context.Context, chat entity.Chat) ([]entity.ProjectRepository, error) {
	bindings, err := svc.cfg.Store.ListChatRepositories(ctx, chat.ID)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return svc.cfg.Store.ListProjectRepositories(ctx, chat.ProjectID)
	}
	repositories := make([]entity.ProjectRepository, 0, len(bindings))
	for _, binding := range bindings {
		repo, err := svc.cfg.Store.GetRepository(ctx, binding.Provider, binding.Owner, binding.Name)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, entity.ProjectRepository{
			ID:            binding.ID,
			ProjectID:     chat.ProjectID,
			RepositoryID:  binding.RepositoryID,
			Provider:      repo.Provider,
			Owner:         repo.Owner,
			Name:          repo.Name,
			DefaultBranch: repo.DefaultBranch,
			IsDefault:     len(repositories) == 0,
		})
	}
	return repositories, nil
}

func (svc *ChatRunService) openAIAccount(ctx context.Context, role entity.AgentRole) (entity.OpenAIAccount, bool) {
	name := strings.TrimSpace(role.OpenAIAccountName)
	if name == "" {
		return entity.OpenAIAccount{}, false
	}
	account, err := svc.cfg.Store.GetOpenAIAccount(ctx, name)
	if err != nil {
		return entity.OpenAIAccount{}, false
	}
	if strings.TrimSpace(account.SecretRef) == "" || strings.EqualFold(account.Status, "disabled") {
		return entity.OpenAIAccount{}, false
	}
	return account, true
}

func (svc *ChatRunService) gitHubAccount(ctx context.Context, role entity.AgentRole, repo entity.ProjectRepository) (entity.GitHubAccount, bool) {
	name := strings.TrimSpace(role.GitHubAccountName)
	if name == "" && repo.Provider == "github" && repo.Owner != "" && repo.Name != "" {
		globalRepo, err := svc.cfg.Store.GetRepository(ctx, repo.Provider, repo.Owner, repo.Name)
		if err == nil {
			name = strings.TrimSpace(globalRepo.GitHubAccountName)
		}
	}
	if name == "" {
		return entity.GitHubAccount{}, false
	}
	account, err := svc.cfg.Store.GetGitHubAccount(ctx, name)
	if err != nil {
		return entity.GitHubAccount{}, false
	}
	if strings.TrimSpace(account.SecretRef) == "" || !strings.EqualFold(account.Status, "configured") {
		return entity.GitHubAccount{}, false
	}
	return account, true
}

func (svc *ChatRunService) postThread(ctx context.Context, command ChatPostCommand, message string) {
	svc.postThreadByID(ctx, command.ChannelID, commandRootPostID(command), message)
}

func (svc *ChatRunService) postThreadByID(ctx context.Context, channelID string, rootPostID string, message string) {
	if svc.cfg.ThreadPublisher == nil || channelID == "" || rootPostID == "" || strings.TrimSpace(message) == "" {
		return
	}
	_, _ = svc.cfg.ThreadPublisher.PostThreadMessage(ctx, MattermostThreadPostInput{
		ChannelID:  channelID,
		RootPostID: rootPostID,
		Message:    message,
	})
}

func (svc *ChatRunService) t(messageID string, data map[string]any) string {
	if svc.cfg.Localizer == nil {
		return messageID
	}
	return svc.cfg.Localizer.T(messageID, data)
}

func (svc *ChatRunService) localeData() promptTemplateLocaleData {
	if svc.cfg.Localizer == nil {
		return promptTemplateLocaleData{Code: "en", Language: "English"}
	}
	return promptTemplateLocaleData{
		Code:     svc.cfg.Localizer.Locale(),
		Language: svc.t("prompt.template.language_name", nil),
	}
}

func normalizeChatPostCommand(command ChatPostCommand) ChatPostCommand {
	command.ChannelID = strings.TrimSpace(command.ChannelID)
	command.PostID = strings.TrimSpace(command.PostID)
	command.RootPostID = strings.TrimSpace(command.RootPostID)
	command.UserID = strings.TrimSpace(command.UserID)
	command.UserName = strings.TrimSpace(command.UserName)
	command.Message = strings.TrimSpace(command.Message)
	return command
}

func commandRootPostID(command ChatPostCommand) string {
	if strings.TrimSpace(command.RootPostID) != "" {
		return strings.TrimSpace(command.RootPostID)
	}
	return strings.TrimSpace(command.PostID)
}

func selectChatRole(roles []entity.AgentRole, prNumber int) entity.AgentRole {
	if prNumber > 0 {
		for _, role := range roles {
			if role.RoleType == "reviewer" {
				return role
			}
		}
	}
	preference := []string{"worker", "manager", "pm_delivery", "analyst", "architect", "writer", "sre", "custom", "reviewer"}
	for _, roleType := range preference {
		for _, role := range roles {
			if role.RoleType == roleType {
				return role
			}
		}
	}
	return roles[0]
}

func chatRunMode(role entity.AgentRole, repositories []entity.ProjectRepository, prNumber int) string {
	if prNumber > 0 && role.RoleType == "reviewer" {
		return chatRunModeReviewer
	}
	if role.RoleType == "worker" || role.RoleType == "sre" {
		return chatRunModeDeveloper
	}
	return chatRunModeChat
}

func extractGitHubPullRequest(message string) githubPullRef {
	matches := githubPullURLRE.FindStringSubmatch(message)
	if len(matches) != 4 {
		return githubPullRef{}
	}
	number, _ := strconv.Atoi(matches[3])
	return githubPullRef{Owner: matches[1], Name: strings.TrimSuffix(matches[2], "."), Number: number}
}

func firstRepository(repositories []entity.ProjectRepository) entity.ProjectRepository {
	if len(repositories) == 0 {
		return entity.ProjectRepository{}
	}
	return repositories[0]
}

func chatRunTitle(message string) string {
	line := firstNonEmptyLine(message)
	if line == "" {
		return "Matter-codex chat task"
	}
	line = truncateMattermostText(line, 96)
	return "Matter-codex chat task: " + line
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func newChatRunID(chatID int64) string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("chat-%d-%d", chatID, time.Now().Unix())
	}
	return fmt.Sprintf("chat-%d-%s", chatID, hex.EncodeToString(raw[:]))
}

func finalAnswerFromLog(logTail string) string {
	const begin = "matter-codex final answer begin"
	const end = "matter-codex final answer end"
	start := strings.LastIndex(logTail, begin)
	if start < 0 {
		return ""
	}
	rest := logTail[start+len(begin):]
	stop := strings.Index(rest, end)
	if stop >= 0 {
		rest = rest[:stop]
	}
	return truncateMattermostText(strings.TrimSpace(rest), 3200)
}

func formatRunArtifacts(artifacts map[string]string) string {
	if len(artifacts) == 0 {
		return "-"
	}
	keys := []string{"pr-url", "branch", "commit", "review-decision", "review-submitted", "no-changes", "local-changes"}
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(artifacts[key])
		if value != "" {
			lines = append(lines, "- "+key+": "+value)
		}
	}
	if len(lines) == 0 {
		return "-"
	}
	return strings.Join(lines, "\n")
}

func truncateMattermostText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "\n..."
}
