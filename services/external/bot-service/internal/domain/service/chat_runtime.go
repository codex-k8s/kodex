package service

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
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

	threadContextStatusPending    = "pending"
	threadContextStatusConfigured = "configured"
	threadContextStatusClosed     = "closed"

	maxAgentSessionCapacityEvictions = 1
	defaultCapacityRetryDelay        = 500 * time.Millisecond
)

var githubPullURLRE = regexp.MustCompile(`https://github\.com/([^/\s]+)/([^/\s]+)/pull/([0-9]+)`)

var codexAuthDeviceCodeWait = 5 * time.Minute

type MattermostThreadPostInput struct {
	ChannelID  string
	RootPostID string
	Message    string
	Props      map[string]any
}

type MattermostThreadUpdateInput struct {
	ChannelID  string
	RootPostID string
	PostID     string
	Message    string
	Props      map[string]any
}

type MattermostPostRef struct {
	ChannelID string
	PostID    string
}

type MattermostPostReactionInput struct {
	PostID    string
	UserID    string
	EmojiName string
}

type MattermostThreadPublisher interface {
	PostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error)
	PostThreadMessageWithToken(ctx context.Context, token string, input MattermostThreadPostInput) (MattermostPostRef, error)
	UpdateThreadMessage(ctx context.Context, input MattermostThreadUpdateInput) (MattermostPostRef, error)
	UpdateThreadMessageWithToken(ctx context.Context, token string, input MattermostThreadUpdateInput) (MattermostPostRef, error)
	PostThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error)
	UpdateThreadCard(ctx context.Context, card MattermostCard) (MattermostPostRef, error)
	AddPostReactionWithToken(ctx context.Context, token string, input MattermostPostReactionInput) error
}

type ChatPostCommand struct {
	ChannelID  string
	PostID     string
	RootPostID string
	UserID     string
	UserName   string
	Message    string
	Props      map[string]any
}

type ChatRunResult struct {
	Ignored bool
	RunID   string
	Mode    string
}

type AgentTurnRequest struct {
	Project        entity.Project
	Chat           entity.Chat
	Role           entity.AgentRole
	Repositories   []entity.ProjectRepository
	UserID         string
	UserName       string
	UserMessage    string
	SourcePostID   string
	ReplyRootID    string
	SessionRootID  string
	SessionScope   string
	TTLSeconds     int
	PreparedPrompt string
	ParentTurnID   int64
}

type AgentTurnRetryRequest struct {
	Session  entity.AgentSession
	Turn     entity.AgentSessionTurn
	UserID   string
	UserName string
}

type AgentTurnQueued struct {
	RunID              string
	TurnID             int64
	SessionKey         string
	Role               entity.AgentRole
	CreatedPod         bool
	WaitingForCapacity bool
	PodName            string
	PVCName            string
}

type AgentSessionRepairResult struct {
	QueuedSessionsEnsured int
	StaleSessionsReset    int
	Failed                int
}

type AgentTurnDispatcher interface {
	EnqueueAgentTurn(ctx context.Context, request AgentTurnRequest) (AgentTurnQueued, error)
	RetryAgentTurn(ctx context.Context, request AgentTurnRetryRequest) (AgentTurnQueued, error)
}

type ThreadRepositorySelectionInput struct {
	ThreadContextID int64
	RepositoryID    int64
	UserID          string
	UserName        string
}

type ThreadRepositorySelectionResult struct {
	Context entity.ThreadContext
	RunID   string
}

type ThreadRepositorySelector interface {
	SelectThreadRepository(ctx context.Context, input ThreadRepositorySelectionInput) (ThreadRepositorySelectionResult, error)
}

type ChatRunServiceConfig struct {
	Localizer          *texti18n.Localizer
	Store              adminrepo.Repository
	RuntimeRunner      runtimerepo.Runner
	ThreadPublisher    MattermostThreadPublisher
	BotServiceURL      string
	MenuActionURL      string
	MattermostSiteURL  string
	StorageReady       bool
	RuntimeReady       bool
	DisableMonitor     bool
	MonitorInterval    time.Duration
	MonitorTimeout     time.Duration
	CapacityRetryDelay time.Duration
}

type ChatRunService struct {
	cfg ChatRunServiceConfig
}

type CodexReauthRequiredError struct {
	AccountName string
	RoleName    string
	DeviceURL   string
	DeviceCode  string
	JobName     string
	PodName     string
}

func (err *CodexReauthRequiredError) Error() string {
	return fmt.Sprintf("Codex auth requires reauthorization for OpenAI account %s", err.AccountName)
}

func NewChatRunService(cfg ChatRunServiceConfig) *ChatRunService {
	if cfg.MonitorInterval <= 0 {
		cfg.MonitorInterval = 15 * time.Second
	}
	if cfg.MonitorTimeout <= 0 {
		cfg.MonitorTimeout = 6 * time.Hour
	}
	if cfg.CapacityRetryDelay <= 0 {
		cfg.CapacityRetryDelay = defaultCapacityRetryDelay
	}
	return &ChatRunService{cfg: cfg}
}

func (svc *ChatRunService) HandleChatPost(ctx context.Context, command ChatPostCommand) ChatRunResult {
	command = normalizeChatPostCommand(command)
	if command.ChannelID == "" || command.PostID == "" || command.Message == "" {
		return ChatRunResult{Ignored: true}
	}
	if isMatterCodexSystemPost(command.Props) {
		return ChatRunResult{Ignored: true}
	}
	if strings.HasPrefix(command.Message, "/") {
		return ChatRunResult{Ignored: true}
	}
	if hasMattermostNoTriggerMarker(command.Message) {
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
	senderIsAgentBot := false
	if command.UserID != "" {
		_, found, err := svc.mattermostBotIdentityByUserID(ctx, chat.ProjectID, command.UserID)
		if err != nil {
			svc.postThread(ctx, command, svc.t("chat.run.sender_lookup_failed", map[string]any{"Error": safeError(err)}))
			return ChatRunResult{}
		}
		if found {
			senderIsAgentBot = true
		}
	}
	if senderIsAgentBot {
		return ChatRunResult{Ignored: true}
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
	rolesByID := make(map[int64]entity.AgentRole, len(roles))
	for _, role := range roles {
		rolesByID[role.ID] = role
	}
	projectRoles, err := svc.cfg.Store.ListAgentRoles(ctx, chat.ProjectID)
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.roles_lookup_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	mentionRolesByID := make(map[int64]entity.AgentRole, len(projectRoles))
	for _, role := range projectRoles {
		if role.Enabled {
			mentionRolesByID[role.ID] = role
		}
	}
	var targets []chatSessionTarget
	var threadContext entity.ThreadContext
	var repositories []entity.ProjectRepository
	var ready bool
	threadContext, repositories, ready, err = svc.threadContextRepositories(ctx, project, chat, command)
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.repositories_lookup_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	if !ready {
		return ChatRunResult{}
	}
	targets, err = svc.routeChatPost(ctx, chat, roles, rolesByID, mentionRolesByID, command, threadContext)
	if err != nil {
		svc.postThread(ctx, command, svc.t("chat.run.route_failed", map[string]any{"Error": safeError(err)}))
		return ChatRunResult{}
	}
	if len(targets) == 0 {
		return ChatRunResult{Ignored: true}
	}
	queued := make([]AgentTurnQueued, 0, len(targets))
	for _, target := range targets {
		svc.addAgentStartReaction(ctx, command, target.Role)
		item, err := svc.EnqueueAgentTurn(ctx, AgentTurnRequest{
			Project:       project,
			Chat:          chat,
			Role:          target.Role,
			Repositories:  repositories,
			UserID:        command.UserID,
			UserName:      command.UserName,
			UserMessage:   command.Message,
			SourcePostID:  command.PostID,
			ReplyRootID:   commandRootPostID(command),
			SessionRootID: target.SessionRootID,
			SessionScope:  target.SessionScope,
			TTLSeconds:    target.TTLSeconds,
		})
		if err != nil {
			var reauthErr *CodexReauthRequiredError
			if errors.As(err, &reauthErr) {
				svc.postThread(ctx, command, svc.t("chat.run.openai_reauth_required", map[string]any{
					"Role":    target.Role.Name,
					"Account": reauthErr.AccountName,
					"URL":     reauthErr.DeviceURL,
					"Code":    reauthErr.DeviceCode,
					"Job":     reauthErr.JobName,
					"Pod":     emptyAsUnknown(reauthErr.PodName),
				}))
				continue
			}
			svc.postThread(ctx, command, svc.t("chat.run.start_failed", map[string]any{"Role": target.Role.Name, "Error": safeError(err)}))
			continue
		}
		if item.WaitingForCapacity {
			svc.postThread(ctx, command, svc.t("chat.run.waiting_for_capacity", map[string]any{
				"Role":  target.Role.Name,
				"RunID": item.RunID,
			}))
		}
		queued = append(queued, item)
	}
	if len(queued) == 0 {
		return ChatRunResult{}
	}
	first := queued[0]
	return ChatRunResult{RunID: first.RunID, Mode: "session"}
}

type chatSessionTarget struct {
	Role          entity.AgentRole
	SessionScope  string
	SessionRootID string
	TTLSeconds    int
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
	RuntimeEnv    []runtimerepo.RuntimeEnvVar
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

func (svc *ChatRunService) EnqueueAgentTurn(ctx context.Context, request AgentTurnRequest) (AgentTurnQueued, error) {
	request.UserMessage = strings.TrimSpace(request.UserMessage)
	request.ReplyRootID = strings.TrimSpace(request.ReplyRootID)
	if request.UserMessage == "" {
		return AgentTurnQueued{}, fmt.Errorf("user message is required")
	}
	if request.Project.ID == 0 || request.Chat.ID == 0 || request.Role.ID == 0 {
		return AgentTurnQueued{}, fmt.Errorf("project, chat and role are required")
	}
	if request.ReplyRootID == "" {
		return AgentTurnQueued{}, fmt.Errorf("Mattermost reply root post id is required")
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return AgentTurnQueued{}, fmt.Errorf("storage is not ready")
	}
	if !svc.cfg.RuntimeReady || svc.cfg.RuntimeRunner == nil {
		return AgentTurnQueued{}, fmt.Errorf("runtime is not ready")
	}
	ttlSeconds := request.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = defaultThreadSessionTTLSeconds
	}
	sessionScope := strings.TrimSpace(request.SessionScope)
	if sessionScope == "" {
		sessionScope = agentSessionScopeThreadRole
	}
	sessionRootID := strings.TrimSpace(request.SessionRootID)
	sessionKey := agentSessionKey(request.Chat.ID, request.Role.ID, sessionScope, sessionRootID)
	if err := svc.authorizeClusterAdminRole(ctx, request.Role, request.Chat.ID, request.Chat.Slug, request.Chat.MattermostChannelID, sessionKey, "agent_turn.enqueue"); err != nil {
		return AgentTurnQueued{}, err
	}
	openAIAccount, ok := svc.openAIAccount(ctx, request.Role)
	if !ok {
		return AgentTurnQueued{}, fmt.Errorf("OpenAI account is required for role %s", request.Role.Name)
	}
	if err := svc.withClusterAdminRuntimeGuard(
		ctx, request.Role, request.Chat.ID, request.Chat.Slug, request.Chat.MattermostChannelID,
		sessionKey, "agent_auth.ensure.side_effect",
		func() error { return svc.ensureCodexAuthSecretReady(ctx, openAIAccount, request.Role) },
	); err != nil {
		return AgentTurnQueued{}, err
	}
	gitHubAccount, gitHubOK := svc.gitHubAccount(ctx, request.Project, request.Role, firstRepository(request.Repositories))
	runtimeVariableBindings, err := svc.cfg.Store.ListAgentRoleRuntimeVariables(ctx, request.Role.ID)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	runtimeVariables := projectRuntimeVariablesFromBindings(runtimeVariableBindings)
	runtimeEnv := runtimeEnvVarsFromBindings(runtimeVariableBindings)
	capabilities, err := agentSessionCapabilitiesJSON(request.Role, request.Repositories, gitHubOK, runtimeVariableBindings)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	existingSession, sessionExists, err := svc.agentSessionExists(ctx, sessionKey)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	if sessionExists {
		if existingSession.Status == agentSessionStatusBlocked || existingSession.Status == agentSessionStatusClosed {
			return AgentTurnQueued{}, fmt.Errorf("agent session is %s; start a new Mattermost thread", existingSession.Status)
		}
		if account := strings.TrimSpace(existingSession.OpenAIAccountName); account != "" && account != openAIAccount.Name {
			return AgentTurnQueued{}, fmt.Errorf("agent session belongs to OpenAI account %s; start a new Mattermost thread", account)
		}
	}
	promptInput := RolePromptInput{
		Project:          request.Project,
		Role:             request.Role,
		Chat:             request.Chat,
		Repositories:     request.Repositories,
		RuntimeVariables: runtimeVariables,
		UserMessage:      request.UserMessage,
		Locale:           svc.localeData(),
	}
	prompt := strings.TrimSpace(request.PreparedPrompt)
	if prompt == "" {
		prompt, err = BuildRolePrompt(promptInput)
		if sessionExists {
			prompt, err = BuildRoleContinuationPrompt(promptInput)
		}
		if err != nil {
			return AgentTurnQueued{}, err
		}
	}
	session, _, err := svc.cfg.Store.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
		SessionKey:           sessionKey,
		ProjectID:            request.Project.ID,
		ChatID:               request.Chat.ID,
		RoleID:               request.Role.ID,
		SessionScope:         sessionScope,
		MattermostChannelID:  request.Chat.MattermostChannelID,
		MattermostRootPostID: sessionRootID,
		OpenAIAccountName:    openAIAccount.Name,
		TTLSeconds:           ttlSeconds,
		Capabilities:         capabilities,
	})
	if err != nil {
		return AgentTurnQueued{}, err
	}
	gitHubSecretName := ""
	if gitHubOK {
		gitHubSecretName = gitHubAccount.SecretRef
	}
	repo := firstRepository(request.Repositories)
	started := agentSessionStartedFromSession(session)
	waitingForCapacity := false
	if agentSessionRuntimeShouldBeEnsured(session) {
		started, err = svc.startAgentSessionRuntime(ctx, session, existingSession, request.Role, openAIAccount.SecretRef, gitHubSecretName, repo, runtimeEnv)
		if err != nil {
			if !runtimerepo.IsAgentSessionCapacityError(err) {
				return AgentTurnQueued{}, err
			}
			waitingForCapacity = true
			started = runtimerepo.StartedAgentSession{SessionKey: session.SessionKey}
		}
		if !waitingForCapacity {
			session, err = svc.cfg.Store.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
				SessionKey:          session.SessionKey,
				Status:              agentSessionStatusIdle,
				KubernetesNamespace: started.Namespace,
				PodName:             started.PodName,
				PVCName:             started.PVCName,
				TokenSecretRef:      started.SecretName,
				ExtendTTLSeconds:    ttlSeconds,
			})
			if err != nil {
				return AgentTurnQueued{}, err
			}
		}
	}
	runID := newChatRunID(request.Chat.ID)
	var turn entity.AgentSessionTurn
	err = svc.withClusterAdminRuntimeGuard(ctx, request.Role, request.Chat.ID, request.Chat.Slug, request.Chat.MattermostChannelID, session.SessionKey, "agent_turn.persist.side_effect", func() error {
		var createErr error
		turn, createErr = svc.cfg.Store.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
			SessionID:            session.ID,
			RunID:                runID,
			MattermostChannelID:  request.Chat.MattermostChannelID,
			MattermostRootPostID: request.ReplyRootID,
			MattermostPostID:     request.SourcePostID,
			ParentTurnID:         request.ParentTurnID,
			UserID:               request.UserID,
			UserName:             request.UserName,
			Message:              prompt,
		})
		if createErr != nil {
			return createErr
		}
		if coordinationStore, ok := svc.cfg.Store.(adminrepo.CoordinationRepository); ok {
			if _, createErr = coordinationStore.EnsureTurnProcess(ctx, adminrepo.EnsureTurnProcessInput{
				TurnID:               turn.ID,
				ParentTurnID:         request.ParentTurnID,
				ProjectID:            request.Project.ID,
				RoleID:               request.Role.ID,
				InitiatorUserID:      request.UserID,
				InitiatorUserName:    request.UserName,
				TriggerPostID:        request.SourcePostID,
				MattermostChannelID:  request.Chat.MattermostChannelID,
				MattermostRootPostID: request.ReplyRootID,
			}); createErr != nil {
				return fmt.Errorf("bind turn to process: %w", createErr)
			}
		}
		if _, createErr = svc.cfg.Store.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
			RunID:               runID,
			FlowID:              "session-" + session.SessionKey,
			ProfileName:         request.Role.Name,
			Role:                request.Role.RoleType,
			Provider:            firstRepository(request.Repositories).Provider,
			Owner:               firstRepository(request.Repositories).Owner,
			Name:                firstRepository(request.Repositories).Name,
			BaseBranch:          defaultString(firstRepository(request.Repositories).DefaultBranch, "main"),
			HeadBranch:          "matter-codex-" + runID,
			Status:              agentSessionTurnQueued,
			KubernetesNamespace: started.Namespace,
			JobName:             started.PodName,
			PVCName:             started.PVCName,
			Summary:             fmt.Sprintf("session turn chat=%d role=%s turn=%d user=%s", request.Chat.ID, request.Role.Name, turn.ID, request.UserName),
		}); createErr != nil {
			return createErr
		}
		_, _ = upsertProjectRunCard(ctx, projectRunCardInput{
			Localizer:         svc.cfg.Localizer,
			Store:             svc.cfg.Store,
			Publisher:         svc.cfg.ThreadPublisher,
			MattermostSiteURL: svc.cfg.MattermostSiteURL,
			Project:           request.Project,
			Session:           session,
			Turn:              turn,
			RoleName:          request.Role.Name,
			OpenAIAccountName: openAIAccount.Name,
			Status:            agentSessionTurnQueued,
		})
		return nil
	})
	if err != nil {
		return AgentTurnQueued{}, err
	}
	return AgentTurnQueued{
		RunID:              runID,
		TurnID:             turn.ID,
		SessionKey:         session.SessionKey,
		Role:               request.Role,
		CreatedPod:         started.Created,
		WaitingForCapacity: waitingForCapacity,
		PodName:            started.PodName,
		PVCName:            started.PVCName,
	}, nil
}

func (svc *ChatRunService) RetryAgentTurn(ctx context.Context, request AgentTurnRetryRequest) (AgentTurnQueued, error) {
	if request.Session.ID == 0 || request.Turn.ID == 0 {
		return AgentTurnQueued{}, fmt.Errorf("session and turn are required")
	}
	project, err := svc.cfg.Store.GetProject(ctx, request.Session.ProjectID)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	chat, err := svc.cfg.Store.GetChat(ctx, request.Session.ChatID)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, request.Session.RoleID)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	repositories, err := svc.chatRepositories(ctx, chat)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	prompt := strings.TrimSpace(request.Turn.Message)
	if strings.TrimSpace(request.Session.CodexSessionID) != "" {
		prompt = svc.t("chat.session.turn.retry.prompt", nil)
	}
	return svc.EnqueueAgentTurn(ctx, AgentTurnRequest{
		Project:        project,
		Chat:           chat,
		Role:           role,
		Repositories:   repositories,
		UserID:         request.UserID,
		UserName:       request.UserName,
		UserMessage:    prompt,
		PreparedPrompt: prompt,
		ParentTurnID:   request.Turn.ID,
		SourcePostID:   request.Turn.MattermostPostID,
		ReplyRootID:    request.Turn.MattermostRootPostID,
		SessionRootID:  request.Session.MattermostRootPostID,
		SessionScope:   request.Session.SessionScope,
		TTLSeconds:     request.Session.TTLSeconds,
	})
}

func (svc *ChatRunService) RepairAgentSessions(ctx context.Context, limit int) (AgentSessionRepairResult, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return AgentSessionRepairResult{}, fmt.Errorf("storage is not ready")
	}
	if !svc.cfg.RuntimeReady || svc.cfg.RuntimeRunner == nil {
		return AgentSessionRepairResult{}, fmt.Errorf("runtime is not ready")
	}
	if limit <= 0 {
		limit = 20
	}
	result := AgentSessionRepairResult{}
	staleSessions, err := svc.cfg.Store.ListStaleActiveAgentSessions(ctx, limit)
	if err != nil {
		return result, err
	}
	for _, session := range staleSessions {
		if strings.TrimSpace(session.PodName) != "" {
			_, _ = svc.cfg.RuntimeRunner.CleanupAgentSession(ctx, session.SessionKey)
		}
		if _, err := svc.cfg.Store.ResetAgentSessionRuntime(ctx, session.SessionKey, agentSessionStatusIdle); err != nil {
			result.Failed++
			continue
		}
		result.StaleSessionsReset++
	}
	runningSessions, err := svc.cfg.Store.ListRunningActiveAgentSessions(ctx, limit)
	if err != nil {
		return result, err
	}
	for _, session := range runningSessions {
		if session.ActiveTurnID == 0 {
			continue
		}
		health, err := svc.cfg.RuntimeRunner.GetAgentSessionRuntimeHealth(ctx, session.SessionKey)
		if err != nil {
			result.Failed++
			continue
		}
		if !health.Terminal {
			continue
		}
		if _, err := svc.cfg.Store.CompleteAgentSessionTurn(ctx, adminrepo.CompleteAgentSessionTurnInput{
			TurnID:       session.ActiveTurnID,
			Status:       agentSessionTurnFailed,
			ErrorMessage: terminalAgentSessionRuntimeError(health),
			Artifacts:    terminalAgentSessionRuntimeArtifacts(health),
		}); err != nil {
			result.Failed++
			continue
		}
		if strings.TrimSpace(session.PodName) != "" || strings.TrimSpace(health.PodName) != "" {
			_, _ = svc.cfg.RuntimeRunner.CleanupAgentSession(ctx, session.SessionKey)
		}
		if _, err := svc.cfg.Store.ResetAgentSessionRuntime(ctx, session.SessionKey, agentSessionStatusIdle); err != nil {
			result.Failed++
			continue
		}
		result.StaleSessionsReset++
	}
	queuedSessions, err := svc.cfg.Store.ListQueuedIdleAgentSessions(ctx, limit)
	if err != nil {
		return result, err
	}
	for _, session := range queuedSessions {
		if err := svc.ensureQueuedAgentSessionRuntime(ctx, session); err != nil {
			result.Failed++
			continue
		}
		result.QueuedSessionsEnsured++
	}
	return result, nil
}

func terminalAgentSessionRuntimeError(health runtimerepo.AgentSessionRuntimeHealth) string {
	reason := strings.TrimSpace(health.Reason)
	if reason == "" {
		reason = strings.TrimSpace(health.Phase)
	}
	if reason == "" {
		reason = "unknown terminal runtime state"
	}
	if health.Exists {
		return fmt.Sprintf("agent runtime pod is terminal: %s", reason)
	}
	return fmt.Sprintf("agent runtime pod is missing: %s", reason)
}

func terminalAgentSessionRuntimeArtifacts(health runtimerepo.AgentSessionRuntimeHealth) string {
	payload := map[string]any{
		"runtime_repair": map[string]any{
			"session_key": health.SessionKey,
			"namespace":   health.Namespace,
			"pod_name":    health.PodName,
			"exists":      health.Exists,
			"phase":       health.Phase,
			"terminal":    health.Terminal,
			"reason":      health.Reason,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (svc *ChatRunService) ensureQueuedAgentSessionRuntime(ctx context.Context, session entity.AgentSession) error {
	project, err := svc.cfg.Store.GetProject(ctx, session.ProjectID)
	if err != nil {
		return err
	}
	chat, err := svc.cfg.Store.GetChat(ctx, session.ChatID)
	if err != nil {
		return err
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, session.RoleID)
	if err != nil {
		return err
	}
	if err := svc.authorizeClusterAdminRole(ctx, role, chat.ID, chat.Slug, chat.MattermostChannelID, session.SessionKey, "agent_session.repair"); err != nil {
		return err
	}
	openAIAccount, ok := svc.openAIAccountForSession(ctx, session, role)
	if !ok {
		return fmt.Errorf("OpenAI account is required for role %s", role.Name)
	}
	if err := svc.ensureCodexAuthSecretReady(ctx, openAIAccount, role); err != nil {
		return err
	}
	repositories, err := svc.chatRepositories(ctx, chat)
	if err != nil {
		return err
	}
	gitHubSecretName := ""
	if gitHubAccount, ok := svc.gitHubAccount(ctx, project, role, firstRepository(repositories)); ok {
		gitHubSecretName = gitHubAccount.SecretRef
	}
	runtimeVariableBindings, err := svc.cfg.Store.ListAgentRoleRuntimeVariables(ctx, role.ID)
	if err != nil {
		return err
	}
	started, err := svc.startAgentSessionRuntime(ctx, session, session, role, openAIAccount.SecretRef, gitHubSecretName, firstRepository(repositories), runtimeEnvVarsFromBindings(runtimeVariableBindings))
	if err != nil {
		return err
	}
	ttlSeconds := session.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = defaultThreadSessionTTLSeconds
	}
	_, err = svc.cfg.Store.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
		SessionKey:          session.SessionKey,
		Status:              agentSessionStatusIdle,
		KubernetesNamespace: started.Namespace,
		PodName:             started.PodName,
		PVCName:             started.PVCName,
		TokenSecretRef:      started.SecretName,
		ExtendTTLSeconds:    ttlSeconds,
	})
	return err
}

func (svc *ChatRunService) startAgentSessionRuntime(ctx context.Context, session entity.AgentSession, tokenSession entity.AgentSession, role entity.AgentRole, codexAuthSecretName string, gitHubSecretName string, repo entity.ProjectRepository, runtimeEnv []runtimerepo.RuntimeEnvVar) (runtimerepo.StartedAgentSession, error) {
	if err := svc.authorizeClusterAdminRole(ctx, role, session.ChatID, "", session.MattermostChannelID, session.SessionKey, "agent_session.start"); err != nil {
		return runtimerepo.StartedAgentSession{}, err
	}
	internalToken, err := svc.sessionInternalToken(ctx, tokenSession)
	if err != nil {
		return runtimerepo.StartedAgentSession{}, err
	}
	release, err := svc.cfg.Store.AcquireAgentSessionCapacityLock(ctx)
	if err != nil {
		return runtimerepo.StartedAgentSession{}, err
	}
	defer release()
	input := runtimerepo.AgentSessionPodInput{
		SessionKey:              session.SessionKey,
		Role:                    role.Name,
		KubernetesAccess:        role.KubernetesAccess,
		BotServiceURL:           svc.botServiceURL(),
		InternalToken:           internalToken,
		CodexAuthSecretName:     codexAuthSecretName,
		GitHubSecretName:        gitHubSecretName,
		RepositoryProvider:      repo.Provider,
		RepositoryOwner:         repo.Owner,
		RepositoryName:          repo.Name,
		RepositoryDefaultBranch: repo.DefaultBranch,
		SandboxMode:             role.SandboxMode,
		ConfigOverlay:           role.ConfigOverlay,
		RuntimeEnv:              runtimeEnv,
	}
	var started runtimerepo.StartedAgentSession
	startRuntime := func() error {
		return svc.withClusterAdminRuntimeGuard(ctx, role, session.ChatID, "", session.MattermostChannelID, session.SessionKey, "agent_session.start.side_effect", func() error {
			var startErr error
			started, startErr = svc.cfg.RuntimeRunner.StartAgentSession(ctx, input)
			return startErr
		})
	}
	err = startRuntime()
	for attempt := 0; runtimerepo.IsAgentSessionCapacityError(err) && attempt < maxAgentSessionCapacityEvictions; attempt++ {
		evicted, evictErr := svc.evictOldestIdleAgentSessionPod(ctx, session.SessionKey)
		if evictErr != nil {
			return runtimerepo.StartedAgentSession{}, fmt.Errorf("evict idle agent session pod: %w", evictErr)
		}
		if !evicted {
			return runtimerepo.StartedAgentSession{}, err
		}
		if waitErr := waitAgentSessionCapacityRetry(ctx, svc.cfg.CapacityRetryDelay); waitErr != nil {
			return runtimerepo.StartedAgentSession{}, waitErr
		}
		err = startRuntime()
	}
	return started, err
}

func (svc *ChatRunService) evictOldestIdleAgentSessionPod(ctx context.Context, targetSessionKey string) (bool, error) {
	candidates, err := svc.cfg.Store.ListEvictableIdleAgentSessions(ctx, 100)
	if err != nil {
		return false, err
	}
	for _, candidate := range candidates {
		if candidate.SessionKey == targetSessionKey || strings.TrimSpace(candidate.PodName) == "" {
			continue
		}
		health, err := svc.cfg.RuntimeRunner.GetAgentSessionRuntimeHealth(ctx, candidate.SessionKey)
		if err != nil {
			return false, err
		}
		if !health.Exists {
			if _, clearErr := svc.cfg.Store.ClearIdleAgentSessionPod(ctx, candidate.SessionKey, candidate.PodName); clearErr != nil && !errors.Is(clearErr, adminrepo.ErrNotFound) {
				return false, clearErr
			}
			continue
		}
		if _, err := svc.cfg.RuntimeRunner.CleanupAgentSession(ctx, candidate.SessionKey); err != nil {
			return false, err
		}
		if _, clearErr := svc.cfg.Store.ClearIdleAgentSessionPod(ctx, candidate.SessionKey, candidate.PodName); clearErr != nil && !errors.Is(clearErr, adminrepo.ErrNotFound) {
			return false, clearErr
		}
		_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
			EventType:    "agent_session_capacity_evicted",
			ActorUser:    "matter-codex",
			ResourceType: "agent_session",
			ResourceName: candidate.SessionKey,
			Summary:      "oldest idle agent session pod removed to free runtime capacity",
		})
		return true, nil
	}
	return false, nil
}

func waitAgentSessionCapacityRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (svc *ChatRunService) SelectThreadRepository(ctx context.Context, input ThreadRepositorySelectionInput) (ThreadRepositorySelectionResult, error) {
	if input.ThreadContextID <= 0 {
		return ThreadRepositorySelectionResult{}, fmt.Errorf("thread context is required")
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return ThreadRepositorySelectionResult{}, fmt.Errorf("storage is not ready")
	}
	threadContext, err := svc.cfg.Store.GetThreadContextByID(ctx, input.ThreadContextID)
	if err != nil {
		return ThreadRepositorySelectionResult{}, err
	}
	if threadContext.Status == threadContextStatusConfigured {
		result, err := svc.replayConfiguredThreadContextIfUnstarted(ctx, threadContext)
		if err != nil {
			return ThreadRepositorySelectionResult{}, err
		}
		return ThreadRepositorySelectionResult{Context: threadContext, RunID: result.RunID}, nil
	}
	if input.RepositoryID > 0 {
		if err := svc.validateThreadRepository(ctx, threadContext, input.RepositoryID); err != nil {
			return ThreadRepositorySelectionResult{}, err
		}
	}
	threadContext, _, err = svc.cfg.Store.UpsertThreadContext(ctx, adminrepo.UpsertThreadContextInput{
		ProjectID:            threadContext.ProjectID,
		ChatID:               threadContext.ChatID,
		MattermostChannelID:  threadContext.MattermostChannelID,
		MattermostRootPostID: threadContext.MattermostRootPostID,
		RepositoryID:         input.RepositoryID,
		Status:               threadContextStatusConfigured,
	})
	if err != nil {
		return ThreadRepositorySelectionResult{}, err
	}
	pending := ChatPostCommand{
		ChannelID:  threadContext.MattermostChannelID,
		PostID:     threadContext.PendingMattermostPostID,
		RootPostID: threadContext.MattermostRootPostID,
		UserID:     threadContext.PendingUserID,
		UserName:   threadContext.PendingUserName,
		Message:    threadContext.PendingMessage,
	}
	result := svc.HandleChatPost(ctx, pending)
	return ThreadRepositorySelectionResult{Context: threadContext, RunID: result.RunID}, nil
}

func (svc *ChatRunService) replayConfiguredThreadContextIfUnstarted(ctx context.Context, threadContext entity.ThreadContext) (ChatRunResult, error) {
	if strings.TrimSpace(threadContext.PendingMattermostPostID) == "" || strings.TrimSpace(threadContext.PendingMessage) == "" {
		return ChatRunResult{Ignored: true}, nil
	}
	sessions, err := svc.cfg.Store.ListAgentSessionsByThread(ctx, threadContext.ChatID, threadContext.MattermostRootPostID)
	if err != nil {
		return ChatRunResult{}, err
	}
	if len(sessions) > 0 {
		return ChatRunResult{Ignored: true}, nil
	}
	return svc.HandleChatPost(ctx, ChatPostCommand{
		ChannelID:  threadContext.MattermostChannelID,
		PostID:     threadContext.PendingMattermostPostID,
		RootPostID: threadContext.MattermostRootPostID,
		UserID:     threadContext.PendingUserID,
		UserName:   threadContext.PendingUserName,
		Message:    threadContext.PendingMessage,
	}), nil
}

func (svc *ChatRunService) validateThreadRepository(ctx context.Context, threadContext entity.ThreadContext, repositoryID int64) error {
	repositories, err := svc.cfg.Store.ListProjectRepositories(ctx, threadContext.ProjectID)
	if err != nil {
		return err
	}
	for _, repo := range repositories {
		if repo.RepositoryID == repositoryID {
			return nil
		}
	}
	return fmt.Errorf("repository is not bound to project")
}

func (svc *ChatRunService) routeChatPost(ctx context.Context, chat entity.Chat, roles []entity.AgentRole, rolesByID map[int64]entity.AgentRole, mentionRolesByID map[int64]entity.AgentRole, command ChatPostCommand, threadContext entity.ThreadContext) ([]chatSessionTarget, error) {
	identities, err := svc.cfg.Store.ListMattermostBotIdentitiesByProject(ctx, chat.ProjectID)
	if err != nil && !errors.Is(err, adminrepo.ErrNotFound) {
		return nil, err
	}
	mentionedRoles := mentionedAgentRoles(command.Message, identities, mentionRolesByID)
	isThreadReply := strings.TrimSpace(command.RootPostID) != "" && strings.TrimSpace(command.RootPostID) != strings.TrimSpace(command.PostID)
	if len(mentionedRoles) > 0 {
		targets := make([]chatSessionTarget, 0, len(mentionedRoles))
		for _, role := range mentionedRoles {
			targets = append(targets, chatSessionTarget{
				Role:          role,
				SessionScope:  agentSessionScopeThreadRole,
				SessionRootID: commandRootPostID(command),
				TTLSeconds:    defaultThreadSessionTTLSeconds,
			})
		}
		return targets, nil
	}
	if isThreadReply {
		sessions, err := svc.cfg.Store.ListAgentSessionsByThread(ctx, chat.ID, commandRootPostID(command))
		if err != nil {
			return nil, err
		}
		if len(sessions) == 1 {
			if role, ok := rolesByID[sessions[0].RoleID]; ok {
				return []chatSessionTarget{{
					Role:          role,
					SessionScope:  sessions[0].SessionScope,
					SessionRootID: sessions[0].MattermostRootPostID,
					TTLSeconds:    sessions[0].TTLSeconds,
				}}, nil
			}
		}
	}
	target := chatSessionTarget{
		Role:          selectDefaultChatRole(roles),
		SessionScope:  agentSessionScopeChatDefault,
		SessionRootID: "",
		TTLSeconds:    defaultManagerSessionTTLSeconds,
	}
	if threadContext.RepositoryID > 0 {
		target.SessionScope = agentSessionScopeThreadRole
		target.SessionRootID = commandRootPostID(command)
		target.TTLSeconds = defaultThreadSessionTTLSeconds
	}
	return []chatSessionTarget{target}, nil
}

func (svc *ChatRunService) addAgentStartReaction(ctx context.Context, command ChatPostCommand, role entity.AgentRole) {
	if svc.cfg.ThreadPublisher == nil || svc.cfg.RuntimeRunner == nil {
		return
	}
	postID := strings.TrimSpace(command.PostID)
	if postID == "" || role.ID == 0 {
		return
	}
	identity, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, role.ID)
	if err != nil || strings.TrimSpace(identity.TokenSecretRef) == "" || strings.TrimSpace(identity.MattermostUserID) == "" {
		return
	}
	secret, err := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, identity.TokenSecretRef)
	if err != nil || strings.TrimSpace(secret.Token) == "" {
		return
	}
	_ = svc.cfg.ThreadPublisher.AddPostReactionWithToken(ctx, secret.Token, MattermostPostReactionInput{
		PostID:    postID,
		UserID:    identity.MattermostUserID,
		EmojiName: "eyes",
	})
}

func (svc *ChatRunService) mattermostBotIdentityByUserID(ctx context.Context, projectID int64, mattermostUserID string) (entity.MattermostBotIdentity, bool, error) {
	mattermostUserID = strings.TrimSpace(mattermostUserID)
	if projectID == 0 || mattermostUserID == "" {
		return entity.MattermostBotIdentity{}, false, nil
	}
	identities, err := svc.cfg.Store.ListMattermostBotIdentitiesByProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return entity.MattermostBotIdentity{}, false, nil
		}
		return entity.MattermostBotIdentity{}, false, err
	}
	for _, identity := range identities {
		if strings.TrimSpace(identity.MattermostUserID) == mattermostUserID {
			return identity, true, nil
		}
	}
	return entity.MattermostBotIdentity{}, false, nil
}

func (svc *ChatRunService) sessionInternalToken(ctx context.Context, session entity.AgentSession) (string, error) {
	if strings.TrimSpace(session.TokenSecretRef) != "" {
		secret, err := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, session.TokenSecretRef)
		if err == nil && strings.TrimSpace(secret.Token) != "" {
			return strings.TrimSpace(secret.Token), nil
		}
	}
	return newInternalToken()
}

func (svc *ChatRunService) agentSessionExists(ctx context.Context, sessionKey string) (entity.AgentSession, bool, error) {
	session, err := svc.cfg.Store.GetAgentSession(ctx, sessionKey)
	if err == nil {
		return session, true, nil
	}
	if errors.Is(err, adminrepo.ErrNotFound) {
		return entity.AgentSession{}, false, nil
	}
	return entity.AgentSession{}, false, err
}

func agentSessionRuntimeReady(session entity.AgentSession) bool {
	return strings.TrimSpace(session.PodName) != "" &&
		strings.TrimSpace(session.PVCName) != "" &&
		strings.TrimSpace(session.TokenSecretRef) != ""
}

func agentSessionRuntimeShouldBeEnsured(session entity.AgentSession) bool {
	if !agentSessionRuntimeReady(session) {
		return true
	}
	return session.ActiveTurnID == 0
}

func agentSessionStartedFromSession(session entity.AgentSession) runtimerepo.StartedAgentSession {
	return runtimerepo.StartedAgentSession{
		SessionKey: session.SessionKey,
		Namespace:  session.KubernetesNamespace,
		PodName:    session.PodName,
		PVCName:    session.PVCName,
		SecretName: session.TokenSecretRef,
		Created:    false,
	}
}

func (svc *ChatRunService) botServiceURL() string {
	return strings.TrimRight(strings.TrimSpace(svc.cfg.BotServiceURL), "/")
}

func (svc *ChatRunService) ensureCodexAuthSecretReady(ctx context.Context, account entity.OpenAIAccount, role entity.AgentRole) error {
	check, err := svc.cfg.RuntimeRunner.CheckCodexAuthSecret(ctx, runtimerepo.CodexAuthSecretCheckInput{
		AccountName: account.Name,
		SecretName:  account.SecretRef,
	})
	if err != nil {
		return fmt.Errorf("check codex auth secret: %w", err)
	}
	if check.Ready {
		if strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
			return nil
		}
		_, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{
			Name:      account.Name,
			SecretRef: account.SecretRef,
			Status:    "authorized",
		})
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}

	status, completed, authErr := svc.startCodexReauthSession(ctx, account)
	if authErr != nil {
		return authErr
	}
	if completed {
		check, err = svc.cfg.RuntimeRunner.CheckCodexAuthSecret(ctx, runtimerepo.CodexAuthSecretCheckInput{
			AccountName: account.Name,
			SecretName:  account.SecretRef,
		})
		if err == nil && check.Ready {
			return nil
		}
	}
	_, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{
		Name:      account.Name,
		SecretRef: account.SecretRef,
		Status:    "awaiting_user",
	})
	return &CodexReauthRequiredError{
		AccountName: account.Name,
		RoleName:    role.Name,
		DeviceURL:   status.DeviceURL,
		DeviceCode:  status.DeviceCode,
		JobName:     status.JobName,
		PodName:     status.PodName,
	}
}

func (svc *ChatRunService) startCodexReauthSession(ctx context.Context, account entity.OpenAIAccount) (runtimerepo.CodexAuthStatus, bool, error) {
	status, err := svc.cfg.RuntimeRunner.GetCodexAuthStatus(ctx, account.Name, account.SecretRef)
	if err == nil && status.Exists {
		if status.AuthReady {
			if err := svc.completeCodexAuthSession(ctx, account); err != nil {
				return status, false, err
			}
			return status, true, nil
		}
		if status.JobActive > 0 && status.DeviceURL != "" && status.DeviceCode != "" {
			return status, false, nil
		}
		_, _ = svc.cfg.RuntimeRunner.CleanupCodexAuthSession(ctx, account.Name)
	}

	if _, err := svc.cfg.RuntimeRunner.StartCodexAuthSession(ctx, runtimerepo.CodexAuthSessionInput{AccountName: account.Name, SecretName: account.SecretRef}); err != nil {
		return runtimerepo.CodexAuthStatus{}, false, err
	}
	deadline := time.Now().Add(codexAuthDeviceCodeWait)
	for {
		status, err = svc.cfg.RuntimeRunner.GetCodexAuthStatus(ctx, account.Name, account.SecretRef)
		if err != nil {
			return runtimerepo.CodexAuthStatus{}, false, err
		}
		if status.AuthReady {
			if err := svc.completeCodexAuthSession(ctx, account); err != nil {
				return status, false, err
			}
			return status, true, nil
		}
		if status.DeviceURL != "" && status.DeviceCode != "" {
			return status, false, nil
		}
		if status.JobFailed > 0 {
			return status, false, fmt.Errorf("codex device-code auth job failed")
		}
		if time.Now().After(deadline) {
			return status, false, fmt.Errorf("codex device-code auth did not provide url/code before timeout: job %s pod %s phase %s", emptyAsUnknown(status.JobName), emptyAsUnknown(status.PodName), emptyAsUnknown(status.PodPhase))
		}
		select {
		case <-ctx.Done():
			return runtimerepo.CodexAuthStatus{}, false, ctx.Err()
		case <-time.After(750 * time.Millisecond):
		}
	}
}

func (svc *ChatRunService) completeCodexAuthSession(ctx context.Context, account entity.OpenAIAccount) error {
	completed, err := svc.cfg.RuntimeRunner.CompleteCodexAuthSession(ctx, runtimerepo.CodexAuthCompleteInput{AccountName: account.Name, SecretName: account.SecretRef})
	if err != nil {
		return err
	}
	_, _ = svc.cfg.Store.UpdateOpenAIAccountStatus(ctx, adminrepo.UpdateOpenAIAccountStatusInput{
		Name:      account.Name,
		SecretRef: completed.SecretName,
		Status:    "authorized",
	})
	_, _ = svc.cfg.RuntimeRunner.CleanupCodexAuthSession(ctx, account.Name)
	return nil
}

func (svc *ChatRunService) startRun(ctx context.Context, input chatRunStartInput) (runtimerepo.StartedRun, error) {
	if err := svc.authorizeClusterAdminRole(ctx, input.Role, input.Chat.ID, input.Chat.Slug, input.Chat.MattermostChannelID, "", "agent_run.start"); err != nil {
		return runtimerepo.StartedRun{}, err
	}
	var started runtimerepo.StartedRun
	err := svc.withClusterAdminRuntimeGuard(ctx, input.Role, input.Chat.ID, input.Chat.Slug, input.Chat.MattermostChannelID, "", "agent_run.start.side_effect", func() error {
		var startErr error
		started, startErr = svc.startRunAuthorized(ctx, input)
		return startErr
	})
	return started, err
}

func (svc *ChatRunService) startRunAuthorized(ctx context.Context, input chatRunStartInput) (runtimerepo.StartedRun, error) {
	repo := firstRepository(input.Repositories)
	switch input.Mode {
	case chatRunModeReviewer:
		return svc.cfg.RuntimeRunner.StartReviewRun(ctx, runtimerepo.ReviewRunInput{
			RunID:               input.RunID,
			Profile:             input.Role.Name,
			KubernetesAccess:    input.Role.KubernetesAccess,
			CodexAuthSecretName: input.OpenAIAccount.SecretRef,
			GitHubSecretName:    input.GitHubAccount.SecretRef,
			Provider:            repo.Provider,
			Owner:               repo.Owner,
			Name:                repo.Name,
			PRNumber:            input.PRNumber,
			Prompt:              input.Prompt,
			SandboxMode:         input.Role.SandboxMode,
			ConfigOverlay:       input.Role.ConfigOverlay,
			RuntimeEnv:          input.RuntimeEnv,
		})
	case chatRunModeDeveloper:
		return svc.cfg.RuntimeRunner.StartDeveloperRun(ctx, runtimerepo.DeveloperRunInput{
			RunID:               input.RunID,
			Profile:             input.Role.Name,
			KubernetesAccess:    input.Role.KubernetesAccess,
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
			RuntimeEnv:          input.RuntimeEnv,
		})
	default:
		gitHubSecret := ""
		if input.GitHubAccount.SecretRef != "" {
			gitHubSecret = input.GitHubAccount.SecretRef
		}
		return svc.cfg.RuntimeRunner.StartChatRun(ctx, runtimerepo.ChatRunInput{
			RunID:               input.RunID,
			Profile:             input.Role.Name,
			KubernetesAccess:    input.Role.KubernetesAccess,
			CodexAuthSecretName: input.OpenAIAccount.SecretRef,
			GitHubSecretName:    gitHubSecret,
			Prompt:              input.Prompt,
			SandboxMode:         input.Role.SandboxMode,
			ConfigOverlay:       input.Role.ConfigOverlay,
			RuntimeEnv:          input.RuntimeEnv,
		})
	}
}

func (svc *ChatRunService) withClusterAdminRuntimeGuard(ctx context.Context, role entity.AgentRole, chatID int64, chatSlug string, channelID string, sessionKey string, operation string, sideEffect func() error) error {
	if !strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
		return sideEffect()
	}
	repository, ok := svc.cfg.Store.(securityrepo.ClusterAdminRuntimeGuardRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	guardedSideEffect := func() error {
		if err := svc.verifyClusterAdminSecretIntegrity(ctx, role.ID, sessionKey); err != nil {
			return err
		}
		return sideEffect()
	}
	return repository.WithExistingClusterAdminRuntimeGuard(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: role.ID, ProjectID: role.ProjectID, ChatID: chatID, ChatSlug: chatSlug,
		MattermostChannelID: channelID, SessionKey: sessionKey, Operation: operation, ActorUser: "runtime",
	}, guardedSideEffect)
}

func (svc *ChatRunService) verifyClusterAdminSecretIntegrity(ctx context.Context, roleID int64, sessionKey string) error {
	repository, ok := svc.cfg.Store.(securityrepo.ClusterAdminSecretIntegrityRepository)
	if !ok || svc.cfg.RuntimeRunner == nil {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	bindings, err := repository.ListClusterAdminSecretIntegrity(ctx, roleID, sessionKey)
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	for _, binding := range bindings {
		if strings.TrimSpace(binding.ContentSHA256) == "" || strings.TrimSpace(binding.ResourceUID) == "" || strings.TrimSpace(binding.ResourceVersion) == "" {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		actual, err := svc.cfg.RuntimeRunner.InspectSecretIntegrity(ctx, runtimerepo.SecretIntegrityInput{
			SecretName: binding.SecretRef,
			SecretKey:  binding.SecretKey,
		})
		if err != nil || actual.ContentSHA256 != binding.ContentSHA256 || actual.UID != binding.ResourceUID {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
	}
	return nil
}

func (svc *ChatRunService) authorizeClusterAdminRole(ctx context.Context, role entity.AgentRole, chatID int64, chatSlug string, channelID string, sessionKey string, operation string) error {
	if !strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
		return nil
	}
	repository, ok := svc.cfg.Store.(securityrepo.Repository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	allowed, err := repository.AdmitExistingClusterAdmin(ctx, securityrepo.ClusterAdminAdmissionInput{
		SubjectType: "agent_role",
		SubjectKey:  strconv.FormatInt(role.ID, 10),
		ProjectID:   role.ProjectID,
		ProfileName: role.Name,
		ActorUser:   "runtime",
		Operation:   operation,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	bindingRepository, ok := svc.cfg.Store.(securityrepo.ClusterAdminBindingRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	allowed, err = bindingRepository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: role.ID, ProjectID: role.ProjectID, ChatID: chatID, ChatSlug: chatSlug, MattermostChannelID: channelID, SessionKey: sessionKey,
		Operation: operation, ActorUser: "runtime",
	})
	if err != nil {
		return err
	}
	if !allowed {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return nil
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

func (svc *ChatRunService) threadContextRepositories(ctx context.Context, project entity.Project, chat entity.Chat, command ChatPostCommand) (entity.ThreadContext, []entity.ProjectRepository, bool, error) {
	rootPostID := commandRootPostID(command)
	threadContext, err := svc.cfg.Store.GetThreadContext(ctx, chat.ID, rootPostID)
	if err == nil {
		if threadContext.Status == threadContextStatusClosed {
			svc.postThread(ctx, command, svc.t("chat.session.closed", nil))
			return threadContext, nil, false, nil
		}
		if threadContext.Status == threadContextStatusPending {
			return threadContext, nil, false, nil
		}
		return threadContext, threadContextRepository(threadContext), true, nil
	}
	if err != nil && !errors.Is(err, adminrepo.ErrNotFound) {
		return entity.ThreadContext{}, nil, false, err
	}
	repositories, err := svc.threadRepositoryOptions(ctx, chat)
	if err != nil {
		return entity.ThreadContext{}, nil, false, err
	}
	sessions, err := svc.cfg.Store.ListAgentSessionsByThread(ctx, chat.ID, rootPostID)
	if err != nil {
		return entity.ThreadContext{}, nil, false, err
	}
	if len(sessions) > 0 {
		threadContext, _, err = svc.cfg.Store.UpsertThreadContext(ctx, adminrepo.UpsertThreadContextInput{
			ProjectID:            project.ID,
			ChatID:               chat.ID,
			MattermostChannelID:  chat.MattermostChannelID,
			MattermostRootPostID: rootPostID,
			RepositoryID:         existingThreadRepositoryID(sessions, repositories),
			Status:               threadContextStatusConfigured,
		})
		if err != nil {
			return entity.ThreadContext{}, nil, false, err
		}
		return threadContext, threadContextRepository(threadContext), true, nil
	}
	if len(repositories) == 0 {
		threadContext, _, err = svc.cfg.Store.UpsertThreadContext(ctx, adminrepo.UpsertThreadContextInput{
			ProjectID:            project.ID,
			ChatID:               chat.ID,
			MattermostChannelID:  chat.MattermostChannelID,
			MattermostRootPostID: rootPostID,
			Status:               threadContextStatusConfigured,
		})
		if err != nil {
			return entity.ThreadContext{}, nil, false, err
		}
		return threadContext, nil, true, nil
	}
	threadContext, created, err := svc.cfg.Store.UpsertThreadContext(ctx, adminrepo.UpsertThreadContextInput{
		ProjectID:               project.ID,
		ChatID:                  chat.ID,
		MattermostChannelID:     chat.MattermostChannelID,
		MattermostRootPostID:    rootPostID,
		Status:                  threadContextStatusPending,
		PendingMattermostPostID: command.PostID,
		PendingUserID:           command.UserID,
		PendingUserName:         command.UserName,
		PendingMessage:          command.Message,
	})
	if err != nil {
		return entity.ThreadContext{}, nil, false, err
	}
	if created {
		if !svc.postThreadRepositoryChoiceCard(ctx, project, threadContext, repositories) {
			threadContext, _, err = svc.cfg.Store.UpsertThreadContext(ctx, adminrepo.UpsertThreadContextInput{
				ProjectID:            project.ID,
				ChatID:               chat.ID,
				MattermostChannelID:  chat.MattermostChannelID,
				MattermostRootPostID: rootPostID,
				Status:               threadContextStatusConfigured,
			})
			if err != nil {
				return entity.ThreadContext{}, nil, false, err
			}
			return threadContext, nil, true, nil
		}
	}
	return threadContext, nil, false, nil
}

func (svc *ChatRunService) threadRepositoryOptions(ctx context.Context, chat entity.Chat) ([]entity.ProjectRepository, error) {
	bindings, err := svc.cfg.Store.ListChatRepositories(ctx, chat.ID)
	if err != nil {
		return nil, err
	}
	if len(bindings) > 0 {
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
	return svc.cfg.Store.ListProjectRepositories(ctx, chat.ProjectID)
}

func existingThreadRepositoryID(sessions []entity.AgentSession, repositories []entity.ProjectRepository) int64 {
	var repositoryID int64
	for _, session := range sessions {
		var capabilities struct {
			Repositories []struct {
				Provider string `json:"provider"`
				Owner    string `json:"owner"`
				Name     string `json:"name"`
			} `json:"repositories"`
		}
		if json.Unmarshal([]byte(session.Capabilities), &capabilities) != nil || len(capabilities.Repositories) == 0 {
			continue
		}
		saved := capabilities.Repositories[0]
		for _, repository := range repositories {
			if !strings.EqualFold(strings.TrimSpace(repository.Provider), strings.TrimSpace(saved.Provider)) ||
				!strings.EqualFold(strings.TrimSpace(repository.Owner), strings.TrimSpace(saved.Owner)) ||
				!strings.EqualFold(strings.TrimSpace(repository.Name), strings.TrimSpace(saved.Name)) {
				continue
			}
			if repositoryID != 0 && repositoryID != repository.RepositoryID {
				return 0
			}
			repositoryID = repository.RepositoryID
			break
		}
	}
	return repositoryID
}

func threadContextRepository(threadContext entity.ThreadContext) []entity.ProjectRepository {
	if threadContext.RepositoryID == 0 || threadContext.RepositoryOwner == "" || threadContext.RepositoryName == "" {
		return nil
	}
	return []entity.ProjectRepository{{
		ProjectID:     threadContext.ProjectID,
		RepositoryID:  threadContext.RepositoryID,
		Provider:      defaultString(threadContext.RepositoryProvider, "github"),
		Owner:         threadContext.RepositoryOwner,
		Name:          threadContext.RepositoryName,
		DefaultBranch: defaultString(threadContext.RepositoryDefaultBranch, "main"),
		IsDefault:     true,
	}}
}

func (svc *ChatRunService) postThreadRepositoryChoiceCard(ctx context.Context, project entity.Project, threadContext entity.ThreadContext, repositories []entity.ProjectRepository) bool {
	if svc.cfg.ThreadPublisher == nil || strings.TrimSpace(svc.cfg.MenuActionURL) == "" {
		svc.postThreadByID(ctx, threadContext.MattermostChannelID, threadContext.MattermostRootPostID, svc.t("chat.thread.repository_choice.text", nil))
		return false
	}
	card := MattermostCard{
		ChannelID:  threadContext.MattermostChannelID,
		RootPostID: threadContext.MattermostRootPostID,
		ActionURL:  svc.cfg.MenuActionURL,
		Message:    svc.t("menu.message", nil),
		Color:      "#1c58d9",
		Title:      svc.t("chat.thread.repository_choice.title", map[string]any{"Project": project.Name}),
		Text:       svc.t("chat.thread.repository_choice.text", map[string]any{"Owner": emptyAsUnknown(project.GitHubOwner)}),
		Interaction: MattermostCardInteraction{
			Actor: AuthenticatedActor{UserID: threadContext.PendingUserID, UserName: threadContext.PendingUserName},
			Scope: InteractionScope{Workspace: strconv.FormatInt(project.ID, 10)},
		},
		Fields: []MattermostCardField{
			{Title: svc.t("menu.entity.field.project", nil), Value: "`" + project.Name + "`", Short: true},
			{Title: svc.t("menu.entity.field.github_owner", nil), Value: "`" + emptyAsUnknown(project.GitHubOwner) + "`", Short: true},
		},
		Actions: []MattermostCardAction{
			threadRepositoryChoiceAction(svc, "threadreponone", threadContext.ID, 0, "chat.thread.repository_choice.none", "chat.thread.repository_choice.none.tooltip", "primary", nil),
		},
	}
	limit := len(repositories)
	if limit > entityListPageSize {
		limit = entityListPageSize
	}
	for idx, repo := range repositories[:limit] {
		card.Actions = append(card.Actions, threadRepositoryChoiceAction(svc, "threadrepo"+strconv.Itoa(idx+1), threadContext.ID, repo.RepositoryID, "chat.thread.repository_choice.repo", "chat.thread.repository_choice.repo.tooltip", "default", map[string]any{"Number": idx + 1, "Repository": repo.FullName()}))
	}
	if _, err := svc.cfg.ThreadPublisher.PostThreadCard(ctx, card); err != nil {
		svc.postThreadByID(ctx, threadContext.MattermostChannelID, threadContext.MattermostRootPostID, svc.t("chat.thread.repository_choice.failed", map[string]any{"Error": safeError(err)}))
		return false
	}
	return true
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

func (svc *ChatRunService) openAIAccountForSession(ctx context.Context, session entity.AgentSession, role entity.AgentRole) (entity.OpenAIAccount, bool) {
	name := strings.TrimSpace(session.OpenAIAccountName)
	if name == "" {
		return svc.openAIAccount(ctx, role)
	}
	account, err := svc.cfg.Store.GetOpenAIAccount(ctx, name)
	if err != nil || strings.TrimSpace(account.SecretRef) == "" || strings.EqualFold(account.Status, "disabled") {
		return entity.OpenAIAccount{}, false
	}
	return account, true
}

func (svc *ChatRunService) gitHubAccount(ctx context.Context, project entity.Project, role entity.AgentRole, repo entity.ProjectRepository) (entity.GitHubAccount, bool) {
	name := strings.TrimSpace(role.GitHubAccountName)
	if name == "" {
		name = strings.TrimSpace(project.GitHubAccountName)
	}
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

func isMatterCodexSystemPost(props map[string]any) bool {
	if len(props) == 0 {
		return false
	}
	value, ok := props["matter_codex_event"]
	if !ok {
		return false
	}
	event, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(event), "agent_")
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
	preference := []string{"worker", "manager", "pm_delivery", "analyst", "architect", "writer", "sre", "custom", "improver", "reviewer"}
	for _, roleType := range preference {
		for _, role := range roles {
			if role.RoleType == roleType {
				return role
			}
		}
	}
	return roles[0]
}

func selectDefaultChatRole(roles []entity.AgentRole) entity.AgentRole {
	preference := []string{"manager", "pm_delivery", "worker", "analyst", "architect", "writer", "sre", "custom", "improver", "reviewer"}
	for _, roleType := range preference {
		for _, role := range roles {
			if role.RoleType == roleType {
				return role
			}
		}
	}
	return roles[0]
}

func mentionedAgentRoles(message string, identities []entity.MattermostBotIdentity, rolesByID map[int64]entity.AgentRole) []entity.AgentRole {
	if len(identities) == 0 || len(rolesByID) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(identities))
	roles := make([]entity.AgentRole, 0, len(identities))
	lowerMessage := strings.ToLower(stripMarkdownCodeForMentionScan(message))
	for _, identity := range identities {
		username := strings.ToLower(strings.TrimSpace(identity.Username))
		if username == "" || !messageMentionsUsername(lowerMessage, username) {
			continue
		}
		role, ok := rolesByID[identity.RoleID]
		if !ok {
			continue
		}
		if _, exists := seen[role.ID]; exists {
			continue
		}
		seen[role.ID] = struct{}{}
		roles = append(roles, role)
	}
	return roles
}

func stripMarkdownCodeForMentionScan(message string) string {
	if !strings.ContainsAny(message, "`~") {
		return message
	}
	var builder strings.Builder
	builder.Grow(len(message))
	inFence := false
	fenceMarker := ""
	for index := 0; index < len(message); {
		if strings.HasPrefix(message[index:], "```") || strings.HasPrefix(message[index:], "~~~") {
			marker := message[index : index+3]
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			builder.WriteByte(' ')
			index += 3
			continue
		}
		if inFence {
			if message[index] == '\n' {
				builder.WriteByte('\n')
			} else {
				builder.WriteByte(' ')
			}
			index++
			continue
		}
		if message[index] == '`' {
			index++
			for index < len(message) && message[index] != '`' {
				if message[index] == '\n' {
					builder.WriteByte('\n')
				} else {
					builder.WriteByte(' ')
				}
				index++
			}
			if index < len(message) {
				index++
			}
			builder.WriteByte(' ')
			continue
		}
		builder.WriteByte(message[index])
		index++
	}
	return builder.String()
}

func messageMentionsUsername(lowerMessage string, username string) bool {
	needle := "@" + username
	searchFrom := 0
	for {
		index := strings.Index(lowerMessage[searchFrom:], needle)
		if index < 0 {
			return false
		}
		start := searchFrom + index
		end := start + len(needle)
		beforeOK := start == 0 || !isMentionUsernameRune(rune(lowerMessage[start-1]))
		afterOK := end >= len(lowerMessage) || !isMentionUsernameRune(rune(lowerMessage[end]))
		if beforeOK && afterOK {
			return true
		}
		searchFrom = end
	}
}

func isMentionUsernameRune(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.'
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

func hasMattermostNoTriggerMarker(message string) bool {
	for _, field := range strings.Fields(strings.ToLower(message)) {
		token := strings.Trim(field, " \t\r\n.,;:!?()[]{}<>\"'`*_~")
		switch token {
		case "#notrigger", "#no-trigger", "#silent":
			return true
		}
	}
	return false
}

func newChatRunID(chatID int64) string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("chat-%d-%d", chatID, time.Now().Unix())
	}
	return fmt.Sprintf("chat-%d-%s", chatID, hex.EncodeToString(raw[:]))
}

func agentSessionKey(chatID int64, roleID int64, scope string, rootPostID string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = agentSessionScopeThreadRole
	}
	if scope == agentSessionScopeChatDefault {
		return fmt.Sprintf("chat-%d-default-role-%d", chatID, roleID)
	}
	hash := sha1.Sum([]byte(rootPostID))
	return fmt.Sprintf("chat-%d-thread-%s-role-%d", chatID, hex.EncodeToString(hash[:8]), roleID)
}

func agentSessionCapabilitiesJSON(role entity.AgentRole, repositories []entity.ProjectRepository, githubEnabled bool, runtimeVariables []entity.AgentRoleRuntimeVariableBinding) (string, error) {
	repos := make([]map[string]string, 0, len(repositories))
	for _, repo := range repositories {
		repos = append(repos, map[string]string{
			"provider":       repo.Provider,
			"owner":          repo.Owner,
			"name":           repo.Name,
			"default_branch": repo.DefaultBranch,
		})
	}
	env := make([]map[string]any, 0, len(runtimeVariables))
	for _, variable := range runtimeVariables {
		if !variable.Enabled || strings.TrimSpace(variable.Name) == "" {
			continue
		}
		env = append(env, map[string]any{
			"name":        variable.Name,
			"description": variable.Description,
			"sensitive":   variable.Sensitive,
		})
	}
	body, err := json.Marshal(map[string]any{
		"mattermost_read":          true,
		"mattermost_post":          true,
		"mattermost_request_agent": true,
		"github_enabled":           githubEnabled,
		"kubernetes_access":        strings.TrimSpace(role.KubernetesAccess),
		"repositories":             repos,
		"runtime_env":              env,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func projectRuntimeVariablesFromBindings(bindings []entity.AgentRoleRuntimeVariableBinding) []entity.ProjectRuntimeVariable {
	items := make([]entity.ProjectRuntimeVariable, 0, len(bindings))
	for _, binding := range bindings {
		if !binding.Enabled {
			continue
		}
		items = append(items, entity.ProjectRuntimeVariable{
			ID:          binding.VariableID,
			ProjectID:   binding.ProjectID,
			Name:        binding.Name,
			Slug:        binding.Slug,
			Description: binding.Description,
			SecretRef:   binding.SecretRef,
			SecretKey:   binding.SecretKey,
			Sensitive:   binding.Sensitive,
			Enabled:     binding.Enabled,
		})
	}
	return items
}

func runtimeEnvVarsFromBindings(bindings []entity.AgentRoleRuntimeVariableBinding) []runtimerepo.RuntimeEnvVar {
	items := make([]runtimerepo.RuntimeEnvVar, 0, len(bindings))
	for _, binding := range bindings {
		if !binding.Enabled || strings.TrimSpace(binding.Name) == "" || strings.TrimSpace(binding.SecretRef) == "" {
			continue
		}
		items = append(items, runtimerepo.RuntimeEnvVar{
			Name:        binding.Name,
			SecretName:  binding.SecretRef,
			SecretKey:   defaultString(binding.SecretKey, "value"),
			Description: binding.Description,
			Sensitive:   binding.Sensitive,
		})
	}
	return items
}

type threadRepositorySelectionState struct {
	ThreadContextID int64 `json:"thread_context_id"`
	RepositoryID    int64 `json:"repository_id"`
}

func threadRepositoryChoiceAction(svc *ChatRunService, actionID string, threadContextID int64, repositoryID int64, nameID string, tooltipID string, style string, data map[string]any) MattermostCardAction {
	return MattermostCardAction{
		ID:      actionID,
		Name:    svc.t(nameID, data),
		Tooltip: svc.t(tooltipID, data),
		Style:   style,
		Context: map[string]any{
			"view":          menuViewChats,
			"action":        menuActionThreadRepositorySelect,
			"resource_type": menuResourceThreadContext,
			"resource_id":   threadRepositorySelectionResourceID(threadContextID, repositoryID),
		},
	}
}

func threadRepositorySelectionResourceID(threadContextID int64, repositoryID int64) string {
	data, err := json.Marshal(threadRepositorySelectionState{ThreadContextID: threadContextID, RepositoryID: repositoryID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func parseThreadRepositorySelectionResourceID(value string) (threadRepositorySelectionState, bool) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return threadRepositorySelectionState{}, false
	}
	var state threadRepositorySelectionState
	if err := json.Unmarshal(data, &state); err != nil {
		return threadRepositorySelectionState{}, false
	}
	if state.ThreadContextID <= 0 || state.RepositoryID < 0 {
		return threadRepositorySelectionState{}, false
	}
	return state, true
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
