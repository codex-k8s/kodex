package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

const (
	agentSessionScopeChatDefault = "chat_default"
	agentSessionScopeThreadRole  = "thread_role"

	agentSessionStatusIdle    = "idle"
	agentSessionStatusRunning = "running"
	agentSessionStatusError   = "error"

	agentSessionTurnQueued    = "queued"
	agentSessionTurnRunning   = "running"
	agentSessionTurnSucceeded = "succeeded"
	agentSessionTurnFailed    = "failed"
	agentSessionTurnCanceled  = "canceled"

	defaultManagerSessionTTLSeconds = 7 * 24 * 60 * 60
	defaultThreadSessionTTLSeconds  = 3 * 24 * 60 * 60
)

type AgentSessionServiceConfig struct {
	Localizer          *texti18n.Localizer
	Store              adminrepo.Repository
	RuntimeRunner      runtimerepo.Runner
	ThreadPublisher    MattermostThreadPublisher
	ConversationReader MattermostConversationReader
	TurnDispatcher     AgentTurnDispatcher
	StorageReady       bool
	RuntimeReady       bool
}

type AgentSessionService struct {
	cfg AgentSessionServiceConfig
}

type MattermostPostMessage struct {
	ID        string `json:"id"`
	RootID    string `json:"root_id"`
	UserID    string `json:"user_id"`
	Message   string `json:"message"`
	CreateAt  int64  `json:"create_at"`
	UpdateAt  int64  `json:"update_at"`
	ChannelID string `json:"channel_id"`
}

type MattermostConversationReader interface {
	GetThreadPosts(ctx context.Context, rootPostID string, limit int) ([]MattermostPostMessage, error)
	SearchChannelPosts(ctx context.Context, channelID string, query string, limit int) ([]MattermostPostMessage, error)
}

type AgentSessionSnapshot struct {
	SessionKey               string `json:"session_key"`
	CodexSessionID           string `json:"codex_session_id"`
	SessionArchiveGzipBase64 string `json:"session_archive_gzip_base64"`
	ExpiresAt                string `json:"expires_at"`
}

type AgentSessionTurnClaim struct {
	HasTurn        bool   `json:"has_turn"`
	Exit           bool   `json:"exit"`
	TurnID         int64  `json:"turn_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	CodexSessionID string `json:"codex_session_id,omitempty"`
	ExpiresAt      string `json:"expires_at"`
}

type CompleteAgentSessionTurnCommand struct {
	TurnID                   int64             `json:"turn_id"`
	RunID                    string            `json:"run_id"`
	Status                   string            `json:"status"`
	FinalMessage             string            `json:"final_message"`
	ErrorMessage             string            `json:"error_message"`
	CodexSessionID           string            `json:"codex_session_id"`
	SessionArchiveGzipBase64 string            `json:"session_archive_gzip_base64"`
	Artifacts                map[string]string `json:"artifacts"`
}

type UpdateAgentSessionTurnStatusCommand struct {
	RunID         string `json:"run_id"`
	Phase         string `json:"phase"`
	OpenAIAccount string `json:"openai_account,omitempty"`
	CodexLimits   string `json:"codex_limits,omitempty"`
}

type StopAgentSessionTurnsCommand struct {
	TurnIDs   []int64
	UserID    string
	UserName  string
	ChannelID string
	PostID    string
}

type StopAgentSessionTurnsResult struct {
	Message string
	Card    *MattermostCard
}

type AgentSessionThreadHistory struct {
	SessionKey string                  `json:"session_key"`
	Posts      []MattermostPostMessage `json:"posts"`
}

type AgentSessionChatSearch struct {
	SessionKey string                  `json:"session_key"`
	Query      string                  `json:"query"`
	Posts      []MattermostPostMessage `json:"posts"`
}

type AgentSessionPostResult struct {
	SessionKey string `json:"session_key"`
	ChannelID  string `json:"channel_id"`
	PostID     string `json:"post_id"`
}

type AgentSessionAgentRequest struct {
	SessionKey        string `json:"session_key"`
	RequestedRunID    string `json:"requested_run_id"`
	RequestedRoleName string `json:"requested_role_name"`
	RequestedRoleID   int64  `json:"requested_role_id"`
	TargetSessionKey  string `json:"target_session_key"`
	AuditPostID       string `json:"audit_post_id,omitempty"`
}

func NewAgentSessionService(cfg AgentSessionServiceConfig) *AgentSessionService {
	return &AgentSessionService{cfg: cfg}
}

func (svc *AgentSessionService) Snapshot(ctx context.Context, sessionKey string, token string) (AgentSessionSnapshot, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	return AgentSessionSnapshot{
		SessionKey:               session.SessionKey,
		CodexSessionID:           session.CodexSessionID,
		SessionArchiveGzipBase64: session.SessionArchiveGzipBase64,
		ExpiresAt:                session.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (svc *AgentSessionService) ClaimNextTurn(ctx context.Context, sessionKey string, token string) (AgentSessionTurnClaim, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionTurnClaim{}, err
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		queued, err := svc.cfg.Store.ListQueuedAgentSessionTurns(ctx, session.ID)
		if err != nil {
			return AgentSessionTurnClaim{}, err
		}
		if len(queued) == 0 {
			return AgentSessionTurnClaim{Exit: true, ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339)}, nil
		}
	}
	turn, err := svc.cfg.Store.ClaimNextAgentSessionTurn(ctx, session.SessionKey)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return AgentSessionTurnClaim{HasTurn: false, ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339)}, nil
		}
		return AgentSessionTurnClaim{}, err
	}
	_, _ = svc.cfg.Store.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
		SessionKey:           session.SessionKey,
		Status:               agentSessionStatusRunning,
		ActiveTurnID:         turn.ID,
		ActiveRunID:          turn.RunID,
		MattermostRootPostID: turn.MattermostRootPostID,
		ExtendTTLSeconds:     session.TTLSeconds,
	})
	_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: turn.RunID, Status: agentSessionTurnRunning})
	_, _ = svc.upsertTurnStatusMessage(ctx, session, turn, svc.turnStartedStatusMessage(ctx, session, turn.RunID, "", ""))
	return AgentSessionTurnClaim{
		HasTurn:        true,
		TurnID:         turn.ID,
		RunID:          turn.RunID,
		Prompt:         turn.Message,
		CodexSessionID: session.CodexSessionID,
		ExpiresAt:      session.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (svc *AgentSessionService) CompleteTurn(ctx context.Context, sessionKey string, token string, command CompleteAgentSessionTurnCommand) error {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return err
	}
	status := command.Status
	if status != agentSessionTurnFailed {
		status = agentSessionTurnSucceeded
	}
	artifacts := "{}"
	if command.Artifacts != nil {
		body, err := json.Marshal(command.Artifacts)
		if err != nil {
			return fmt.Errorf("marshal turn artifacts: %w", err)
		}
		artifacts = string(body)
	}
	turn, err := svc.cfg.Store.CompleteAgentSessionTurn(ctx, adminrepo.CompleteAgentSessionTurnInput{
		TurnID:       command.TurnID,
		Status:       status,
		FinalMessage: command.FinalMessage,
		ErrorMessage: command.ErrorMessage,
		Artifacts:    artifacts,
	})
	if err != nil {
		return err
	}
	sessionStatus := agentSessionStatusIdle
	if status == agentSessionTurnFailed {
		sessionStatus = agentSessionStatusError
	}
	_, _ = svc.cfg.Store.UpdateAgentSessionSnapshot(ctx, adminrepo.UpdateAgentSessionSnapshotInput{
		SessionKey:               session.SessionKey,
		CodexSessionID:           command.CodexSessionID,
		SessionArchiveGzipBase64: command.SessionArchiveGzipBase64,
		Status:                   sessionStatus,
		ExtendTTLSeconds:         session.TTLSeconds,
	})
	prURL := ""
	if command.Artifacts != nil {
		prURL = command.Artifacts["pr-url"]
	}
	_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: turn.RunID, Status: status, PRURL: prURL})
	_, _ = svc.upsertTurnStatusMessage(ctx, session, turn, svc.turnCompletionStatusMessage(ctx, session, status, turn.RunID, command.Artifacts))
	if err := svc.postTurnResult(ctx, session, turn, status, command); err != nil {
		return err
	}
	if status == agentSessionTurnSucceeded {
		_ = svc.postTurnCompletionFYI(ctx, session, turn)
	}
	return nil
}

func (svc *AgentSessionService) StopAgentSessionTurns(ctx context.Context, command StopAgentSessionTurnsCommand) (StopAgentSessionTurnsResult, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return StopAgentSessionTurnsResult{}, fmt.Errorf("storage is not ready")
	}
	if len(command.TurnIDs) == 0 {
		return StopAgentSessionTurnsResult{}, fmt.Errorf("turn id is required")
	}
	seen := make(map[int64]struct{}, len(command.TurnIDs))
	stopped := 0
	skipped := 0
	for _, turnID := range command.TurnIDs {
		if turnID <= 0 {
			continue
		}
		if _, exists := seen[turnID]; exists {
			continue
		}
		seen[turnID] = struct{}{}
		turn, err := svc.cfg.Store.GetAgentSessionTurn(ctx, turnID)
		if err != nil {
			if errors.Is(err, adminrepo.ErrNotFound) {
				skipped++
				continue
			}
			return StopAgentSessionTurnsResult{}, err
		}
		if !agentSessionTurnStoppable(turn.Status) {
			skipped++
			continue
		}
		session, err := svc.cfg.Store.GetAgentSessionByID(ctx, turn.SessionID)
		if err != nil {
			return StopAgentSessionTurnsResult{}, err
		}
		artifacts := "{}"
		if strings.TrimSpace(command.UserName) != "" || strings.TrimSpace(command.UserID) != "" {
			body, err := json.Marshal(map[string]string{
				"stopped-by":         strings.TrimSpace(command.UserName),
				"stopped-by-user-id": strings.TrimSpace(command.UserID),
			})
			if err != nil {
				return StopAgentSessionTurnsResult{}, err
			}
			artifacts = string(body)
		}
		canceled, err := svc.cfg.Store.CancelAgentSessionTurn(ctx, adminrepo.CancelAgentSessionTurnInput{
			TurnID:       turn.ID,
			ErrorMessage: svc.t("chat.session.turn.stop.reason", map[string]any{"User": emptyAsUnknown(command.UserName)}),
			Artifacts:    artifacts,
		})
		if err != nil {
			if errors.Is(err, adminrepo.ErrNotFound) {
				skipped++
				continue
			}
			return StopAgentSessionTurnsResult{}, err
		}
		_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: canceled.RunID, Status: agentSessionTurnCanceled})
		cleanupRuntime := turn.Status == agentSessionTurnRunning || session.ActiveTurnID == turn.ID
		if !cleanupRuntime && turn.Status == agentSessionTurnQueued && session.ActiveTurnID == 0 && agentSessionRuntimeReady(session) {
			queued, err := svc.cfg.Store.ListQueuedAgentSessionTurns(ctx, session.ID)
			if err != nil {
				return StopAgentSessionTurnsResult{}, err
			}
			cleanupRuntime = len(queued) == 0
		}
		if cleanupRuntime {
			if svc.cfg.RuntimeRunner != nil && strings.TrimSpace(session.PodName) != "" {
				_, _ = svc.cfg.RuntimeRunner.CleanupAgentSession(ctx, session.SessionKey)
			}
			_, _ = svc.cfg.Store.ResetAgentSessionRuntime(ctx, session.SessionKey, agentSessionStatusIdle)
		}
		_, _ = svc.upsertTurnStatusMessage(ctx, session, canceled, svc.turnStatusMessage(agentSessionTurnCanceled, canceled.RunID, svc.sessionOpenAIAccountName(ctx, session), ""))
		stopped++
	}
	message := svc.t("chat.session.turn.stop.result", map[string]any{"Stopped": stopped, "Skipped": skipped})
	result := StopAgentSessionTurnsResult{Message: message}
	if strings.TrimSpace(command.ChannelID) != "" && strings.TrimSpace(command.PostID) != "" {
		result.Card = &MattermostCard{
			ChannelID: command.ChannelID,
			PostID:    command.PostID,
			Color:     "#9aa4b2",
			Title:     svc.t("chat.session.turn.stop.title", nil),
			Text:      message,
		}
	}
	return result, nil
}

func (svc *AgentSessionService) ThreadHistory(ctx context.Context, sessionKey string, token string, limit int) (AgentSessionThreadHistory, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionThreadHistory{}, err
	}
	if svc.cfg.ConversationReader == nil {
		return AgentSessionThreadHistory{}, fmt.Errorf("Mattermost conversation reader is not configured")
	}
	rootPostID := strings.TrimSpace(session.MattermostRootPostID)
	if rootPostID == "" {
		return AgentSessionThreadHistory{}, fmt.Errorf("session is not bound to a Mattermost thread")
	}
	posts, err := svc.cfg.ConversationReader.GetThreadPosts(ctx, rootPostID, boundedMCPPostLimit(limit))
	if err != nil {
		return AgentSessionThreadHistory{}, err
	}
	return AgentSessionThreadHistory{SessionKey: session.SessionKey, Posts: posts}, nil
}

func (svc *AgentSessionService) SearchChat(ctx context.Context, sessionKey string, token string, query string, limit int) (AgentSessionChatSearch, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionChatSearch{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return AgentSessionChatSearch{}, fmt.Errorf("query is required")
	}
	if svc.cfg.ConversationReader == nil {
		return AgentSessionChatSearch{}, fmt.Errorf("Mattermost conversation reader is not configured")
	}
	posts, err := svc.cfg.ConversationReader.SearchChannelPosts(ctx, session.MattermostChannelID, query, boundedMCPPostLimit(limit))
	if err != nil {
		return AgentSessionChatSearch{}, err
	}
	return AgentSessionChatSearch{SessionKey: session.SessionKey, Query: query, Posts: posts}, nil
}

func (svc *AgentSessionService) PostThreadUpdate(ctx context.Context, sessionKey string, token string, message string) (AgentSessionPostResult, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return AgentSessionPostResult{}, fmt.Errorf("message is required")
	}
	rootPostID := strings.TrimSpace(session.MattermostRootPostID)
	if rootPostID == "" {
		return AgentSessionPostResult{}, fmt.Errorf("session is not bound to a Mattermost thread")
	}
	if svc.cfg.ThreadPublisher == nil {
		return AgentSessionPostResult{}, fmt.Errorf("Mattermost thread publisher is not configured")
	}
	ref, err := svc.postSessionThreadMessage(ctx, session, rootPostID, message)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	return AgentSessionPostResult{SessionKey: session.SessionKey, ChannelID: ref.ChannelID, PostID: ref.PostID}, nil
}

func (svc *AgentSessionService) UpdateTurnStatus(ctx context.Context, sessionKey string, token string, message string) (AgentSessionPostResult, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return AgentSessionPostResult{}, fmt.Errorf("message is required")
	}
	if session.ActiveTurnID == 0 {
		return AgentSessionPostResult{}, fmt.Errorf("session has no active turn")
	}
	turn, err := svc.cfg.Store.GetAgentSessionTurn(ctx, session.ActiveTurnID)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	ref, err := svc.upsertTurnStatusMessage(ctx, session, turn, message)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	return AgentSessionPostResult{SessionKey: session.SessionKey, ChannelID: ref.ChannelID, PostID: ref.PostID}, nil
}

func (svc *AgentSessionService) UpdateTurnSystemStatus(ctx context.Context, sessionKey string, token string, command UpdateAgentSessionTurnStatusCommand) (AgentSessionPostResult, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	if session.ActiveTurnID == 0 {
		return AgentSessionPostResult{}, fmt.Errorf("session has no active turn")
	}
	turn, err := svc.cfg.Store.GetAgentSessionTurn(ctx, session.ActiveTurnID)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	phase := strings.TrimSpace(command.Phase)
	if phase == "" {
		phase = agentSessionStatusRunning
	}
	runID := defaultString(strings.TrimSpace(command.RunID), turn.RunID)
	message := svc.turnStartedStatusMessage(ctx, session, runID, command.OpenAIAccount, command.CodexLimits)
	if phase == agentSessionTurnFailed || phase == agentSessionTurnSucceeded {
		message = svc.turnStatusMessage(phase, runID, command.OpenAIAccount, command.CodexLimits)
	}
	ref, err := svc.upsertTurnStatusMessage(ctx, session, turn, message)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	return AgentSessionPostResult{SessionKey: session.SessionKey, ChannelID: ref.ChannelID, PostID: ref.PostID}, nil
}

func (svc *AgentSessionService) RequestAgent(ctx context.Context, sessionKey string, token string, target string, message string) (AgentSessionAgentRequest, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	if svc.cfg.TurnDispatcher == nil {
		return AgentSessionAgentRequest{}, fmt.Errorf("agent turn dispatcher is not configured")
	}
	target = strings.TrimSpace(strings.TrimPrefix(target, "@"))
	message = strings.TrimSpace(message)
	if target == "" {
		return AgentSessionAgentRequest{}, fmt.Errorf("target agent is required")
	}
	if message == "" {
		return AgentSessionAgentRequest{}, fmt.Errorf("message is required")
	}
	project, err := svc.cfg.Store.GetProject(ctx, session.ProjectID)
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	chat, err := svc.cfg.Store.GetChat(ctx, session.ChatID)
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	role, err := svc.resolveRequestedRole(ctx, session.ProjectID, target)
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	repositories, err := svc.chatRepositories(ctx, chat)
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	rootPostID := strings.TrimSpace(session.MattermostRootPostID)
	if rootPostID == "" {
		return AgentSessionAgentRequest{}, fmt.Errorf("source session is not bound to a Mattermost thread")
	}
	requesterUserName := svc.sessionMattermostUsername(ctx, session)
	queued, err := svc.cfg.TurnDispatcher.EnqueueAgentTurn(ctx, AgentTurnRequest{
		Project:       project,
		Chat:          chat,
		Role:          role,
		Repositories:  repositories,
		UserName:      requesterUserName,
		UserMessage:   message,
		SourcePostID:  rootPostID,
		ReplyRootID:   rootPostID,
		SessionRootID: rootPostID,
		SessionScope:  agentSessionScopeThreadRole,
		TTLSeconds:    defaultThreadSessionTTLSeconds,
	})
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	auditPostID := ""
	if ref, err := svc.postAgentRequestAudit(ctx, session, rootPostID, requesterUserName, role.Name, message); err == nil {
		auditPostID = ref.PostID
	}
	return AgentSessionAgentRequest{
		SessionKey:        session.SessionKey,
		RequestedRunID:    queued.RunID,
		RequestedRoleName: role.Name,
		RequestedRoleID:   role.ID,
		TargetSessionKey:  queued.SessionKey,
		AuditPostID:       auditPostID,
	}, nil
}

func (svc *AgentSessionService) authorize(ctx context.Context, sessionKey string, token string) (entity.AgentSession, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return entity.AgentSession{}, fmt.Errorf("storage is not ready")
	}
	if !svc.cfg.RuntimeReady || svc.cfg.RuntimeRunner == nil {
		return entity.AgentSession{}, fmt.Errorf("runtime is not ready")
	}
	session, err := svc.cfg.Store.GetAgentSession(ctx, strings.TrimSpace(sessionKey))
	if err != nil {
		return entity.AgentSession{}, err
	}
	if strings.TrimSpace(session.TokenSecretRef) == "" {
		return entity.AgentSession{}, fmt.Errorf("session token secret is not configured")
	}
	secret, err := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, session.TokenSecretRef)
	if err != nil {
		return entity.AgentSession{}, err
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(strings.TrimSpace(secret.Token))) != 1 {
		return entity.AgentSession{}, fmt.Errorf("session token is invalid")
	}
	return session, nil
}

func (svc *AgentSessionService) resolveRequestedRole(ctx context.Context, projectID int64, target string) (entity.AgentRole, error) {
	identities, err := svc.cfg.Store.ListMattermostBotIdentitiesByProject(ctx, projectID)
	if err != nil {
		return entity.AgentRole{}, err
	}
	for _, identity := range identities {
		if strings.EqualFold(identity.Username, target) {
			return svc.cfg.Store.GetAgentRole(ctx, identity.RoleID)
		}
	}
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, projectID)
	if err != nil {
		return entity.AgentRole{}, err
	}
	for _, role := range roles {
		if strings.EqualFold(role.Name, target) {
			return role, nil
		}
	}
	return entity.AgentRole{}, fmt.Errorf("agent role %q was not found", target)
}

func (svc *AgentSessionService) chatRepositories(ctx context.Context, chat entity.Chat) ([]entity.ProjectRepository, error) {
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

func (svc *AgentSessionService) postTurnResult(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, status string, command CompleteAgentSessionTurnCommand) error {
	if svc.cfg.ThreadPublisher == nil {
		return nil
	}
	message := strings.TrimSpace(command.FinalMessage)
	if status == agentSessionTurnFailed {
		message = strings.TrimSpace(command.ErrorMessage)
		if message == "" {
			message = svc.t("chat.run.failed_short", map[string]any{"RunID": turn.RunID})
		}
	}
	if message == "" {
		message = svc.t("chat.run.final_empty", nil)
	}
	_, err := svc.postSessionThreadMessageOnly(ctx, session, turn.MattermostChannelID, turn.MattermostRootPostID, message)
	return err
}

func (svc *AgentSessionService) postTurnCompletionFYI(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn) error {
	if svc.cfg.ThreadPublisher == nil {
		return nil
	}
	userName := mentionableMattermostUsername(turn.UserName)
	if userName == "" {
		return nil
	}
	if svc.turnRequesterIsProjectBot(ctx, session.ProjectID, userName) {
		return nil
	}
	message := svc.t("chat.session.fyi.complete", map[string]any{"UserName": userName})
	_, err := svc.postSessionThreadMessageOnly(ctx, session, turn.MattermostChannelID, turn.MattermostRootPostID, message)
	return err
}

func (svc *AgentSessionService) turnRequesterIsProjectBot(ctx context.Context, projectID int64, userName string) bool {
	if projectID == 0 || strings.TrimSpace(userName) == "" || svc.cfg.Store == nil {
		return false
	}
	identities, err := svc.cfg.Store.ListMattermostBotIdentitiesByProject(ctx, projectID)
	if err != nil {
		return false
	}
	for _, identity := range identities {
		if strings.EqualFold(strings.TrimSpace(identity.Username), userName) {
			return true
		}
	}
	return false
}

func (svc *AgentSessionService) upsertTurnStatusMessage(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, message string) (MattermostPostRef, error) {
	if svc.cfg.ThreadPublisher == nil {
		return MattermostPostRef{}, fmt.Errorf("Mattermost thread publisher is not configured")
	}
	channelID := defaultString(turn.MattermostChannelID, session.MattermostChannelID)
	rootPostID := defaultString(turn.MattermostRootPostID, session.MattermostRootPostID)
	if channelID == "" || rootPostID == "" {
		return MattermostPostRef{}, fmt.Errorf("session turn is not bound to a Mattermost thread")
	}
	message = truncateMattermostStatus(strings.TrimSpace(message))
	if message == "" {
		return MattermostPostRef{}, fmt.Errorf("message is required")
	}
	var ref MattermostPostRef
	var err error
	if strings.TrimSpace(turn.MattermostStatusPostID) == "" {
		ref, err = svc.postSessionThreadMessageOnly(ctx, session, channelID, rootPostID, message)
		if err != nil {
			return MattermostPostRef{}, err
		}
		_, err := svc.cfg.Store.UpdateAgentSessionTurnStatusPost(ctx, adminrepo.UpdateAgentSessionTurnStatusPostInput{
			TurnID:       turn.ID,
			StatusPostID: ref.PostID,
		})
		if err != nil {
			return MattermostPostRef{}, err
		}
	} else {
		ref, err = svc.updateSessionThreadMessageOnly(ctx, session, channelID, rootPostID, turn.MattermostStatusPostID, message)
		if err != nil {
			return MattermostPostRef{}, err
		}
	}
	return ref, nil
}

func (svc *AgentSessionService) postSessionThreadMessage(ctx context.Context, session entity.AgentSession, rootPostID string, message string) (MattermostPostRef, error) {
	return svc.postSessionThreadMessageOnly(ctx, session, session.MattermostChannelID, rootPostID, message)
}

func (svc *AgentSessionService) postAgentRequestAudit(ctx context.Context, session entity.AgentSession, rootPostID string, requesterUserName string, targetRoleName string, message string) (MattermostPostRef, error) {
	if svc.cfg.ThreadPublisher == nil {
		return MattermostPostRef{}, nil
	}
	requester := mentionableMattermostUsername(requesterUserName)
	if requester == "" {
		requester = "agent"
	} else {
		requester = "@" + requester
	}
	target := mentionableMattermostUsername(targetRoleName)
	if target == "" {
		target = strings.TrimSpace(targetRoleName)
	}
	if target == "" {
		target = "agent"
	} else {
		target = "@" + target
	}
	return svc.cfg.ThreadPublisher.PostThreadMessage(ctx, MattermostThreadPostInput{
		ChannelID:  session.MattermostChannelID,
		RootPostID: rootPostID,
		Message:    agentRequestAuditMessage(requester, target, message),
		Props: map[string]any{
			"matter_codex_event": "agent_request",
			"source_agent":       strings.TrimPrefix(requester, "@"),
			"target_agent":       strings.TrimPrefix(target, "@"),
		},
	})
}

func (svc *AgentSessionService) postSessionThreadMessageOnly(ctx context.Context, session entity.AgentSession, channelID string, rootPostID string, message string) (MattermostPostRef, error) {
	input := MattermostThreadPostInput{
		ChannelID:  channelID,
		RootPostID: rootPostID,
		Message:    message,
	}
	identity, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, session.RoleID)
	if err != nil || identity.TokenSecretRef == "" {
		return svc.cfg.ThreadPublisher.PostThreadMessage(ctx, input)
	}
	secret, err := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, identity.TokenSecretRef)
	if err != nil {
		return svc.cfg.ThreadPublisher.PostThreadMessage(ctx, input)
	}
	return svc.cfg.ThreadPublisher.PostThreadMessageWithToken(ctx, secret.Token, input)
}

func agentRequestAuditMessage(requester string, target string, message string) string {
	fence := markdownFence(strings.TrimSpace(message))
	var body strings.Builder
	body.WriteString("matter-codex: ")
	body.WriteString(requester)
	body.WriteString(" запустил ")
	body.WriteString(target)
	body.WriteString(" с prompt:\n\n")
	body.WriteString(fence)
	body.WriteString("markdown\n")
	body.WriteString(strings.TrimSpace(message))
	body.WriteString("\n")
	body.WriteString(fence)
	return body.String()
}

func markdownFence(body string) string {
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return fence
}

func (svc *AgentSessionService) updateSessionThreadMessageOnly(ctx context.Context, session entity.AgentSession, channelID string, rootPostID string, postID string, message string) (MattermostPostRef, error) {
	input := MattermostThreadUpdateInput{
		ChannelID:  channelID,
		RootPostID: rootPostID,
		PostID:     postID,
		Message:    message,
	}
	identity, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, session.RoleID)
	if err != nil || identity.TokenSecretRef == "" {
		return svc.cfg.ThreadPublisher.UpdateThreadMessage(ctx, input)
	}
	secret, err := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, identity.TokenSecretRef)
	if err != nil {
		return svc.cfg.ThreadPublisher.UpdateThreadMessage(ctx, input)
	}
	return svc.cfg.ThreadPublisher.UpdateThreadMessageWithToken(ctx, secret.Token, input)
}

func (svc *AgentSessionService) turnStartedStatusMessage(ctx context.Context, session entity.AgentSession, runID string, openAIAccount string, codexLimits string) string {
	return svc.turnStatusMessage(agentSessionTurnRunning, runID, defaultString(openAIAccount, svc.sessionOpenAIAccountName(ctx, session)), codexLimits)
}

func (svc *AgentSessionService) turnCompletionStatusMessage(ctx context.Context, session entity.AgentSession, status string, runID string, artifacts map[string]string) string {
	openAIAccount := ""
	codexLimits := ""
	if artifacts != nil {
		openAIAccount = strings.TrimSpace(artifacts["openai-account"])
		codexLimits = strings.TrimSpace(artifacts["codex-limits"])
	}
	return svc.turnStatusMessage(status, runID, defaultString(openAIAccount, svc.sessionOpenAIAccountName(ctx, session)), codexLimits)
}

func (svc *AgentSessionService) turnStatusMessage(status string, runID string, openAIAccount string, codexLimits string) string {
	data := map[string]any{"RunID": runID}
	if strings.TrimSpace(openAIAccount) != "" {
		data["OpenAIAccount"] = strings.TrimSpace(openAIAccount)
	}
	if strings.TrimSpace(codexLimits) != "" {
		data["CodexLimits"] = strings.TrimSpace(codexLimits)
	}
	if status == agentSessionTurnFailed {
		return svc.t("chat.session.status.failed", data)
	}
	if status == agentSessionTurnCanceled {
		return svc.t("chat.session.status.canceled", data)
	}
	if status == agentSessionTurnRunning {
		return svc.t("chat.session.status.started", data)
	}
	return svc.t("chat.session.status.succeeded", data)
}

func (svc *AgentSessionService) sessionOpenAIAccountName(ctx context.Context, session entity.AgentSession) string {
	role, err := svc.cfg.Store.GetAgentRole(ctx, session.RoleID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(role.OpenAIAccountName)
}

func (svc *AgentSessionService) sessionMattermostUsername(ctx context.Context, session entity.AgentSession) string {
	identity, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, session.RoleID)
	if err == nil && strings.TrimSpace(identity.Username) != "" {
		return strings.TrimSpace(identity.Username)
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, session.RoleID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(role.Name)
}

func agentSessionTurnStoppable(status string) bool {
	return status == agentSessionTurnQueued || status == agentSessionTurnRunning
}

func mentionableMattermostUsername(userName string) string {
	userName = strings.TrimPrefix(strings.TrimSpace(userName), "@")
	if userName == "" {
		return ""
	}
	for _, char := range userName {
		if !isMentionUsernameRune(char) {
			return ""
		}
	}
	return userName
}

func (svc *AgentSessionService) t(messageID string, data map[string]any) string {
	if svc.cfg.Localizer == nil {
		return messageID
	}
	return svc.cfg.Localizer.T(messageID, data)
}

func newInternalToken() (string, error) {
	var body [32]byte
	if _, err := rand.Read(body[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(body[:]), nil
}

func boundedMCPPostLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func truncateMattermostStatus(value string) string {
	const limit = 3800
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "\n..."
}
