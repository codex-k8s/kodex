package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	agentSessionStatusBlocked = "blocked"
	agentSessionStatusClosed  = "closed"

	agentSessionTurnQueued    = "queued"
	agentSessionTurnRunning   = "running"
	agentSessionTurnSucceeded = "succeeded"
	agentSessionTurnFailed    = "failed"
	agentSessionTurnCanceled  = "canceled"
	agentSessionTurnRetrying  = "capacity_retry"
	agentSessionTurnBlocked   = "blocked"

	agentTurnArtifactCapacityRetriesExhausted = "codex-capacity-retries-exhausted"
	agentTurnArtifactCapacityRetryCount       = "codex-capacity-retry-count"
	agentTurnArtifactFailureCode              = "failure-code"
	agentTurnFailureProviderPolicyBlocked     = "provider-policy-blocked"

	defaultManagerSessionTTLSeconds = 4 * 60 * 60
	defaultThreadSessionTTLSeconds  = 4 * 60 * 60

	defaultMattermostPostMessageMaxRunes = 65535 / 4
	mattermostPostChunkReserveRunes      = 128
	minMattermostPostChunkRunes          = 1000
)

type AgentSessionServiceConfig struct {
	Localizer          *texti18n.Localizer
	Store              adminrepo.Repository
	RuntimeRunner      runtimerepo.Runner
	ThreadPublisher    MattermostThreadPublisher
	ConversationReader MattermostConversationReader
	RoleBotManager     MattermostRoleBotManager
	TurnDispatcher     AgentTurnDispatcher
	MenuActionURL      string
	MattermostSiteURL  string
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

type mattermostPostLimitReader interface {
	MattermostPostMessageMaxRunes(ctx context.Context) (int, error)
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
	RunID             string `json:"run_id"`
	Phase             string `json:"phase"`
	OpenAIAccount     string `json:"openai_account,omitempty"`
	CodexLimits       string `json:"codex_limits,omitempty"`
	RetryAttempt      int    `json:"retry_attempt,omitempty"`
	RetryMaxAttempts  int    `json:"retry_max_attempts,omitempty"`
	RetryDelaySeconds int    `json:"retry_delay_seconds,omitempty"`
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

type RetryAgentSessionTurnCommand struct {
	TurnID    int64
	UserID    string
	UserName  string
	ChannelID string
	PostID    string
}

type RetryAgentSessionTurnResult struct {
	Message string
	RunID   string
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
	if session.Status == agentSessionStatusBlocked || session.Status == agentSessionStatusClosed {
		return AgentSessionTurnClaim{Exit: true, ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339)}, nil
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
	_, _ = svc.upsertTurnStatusCard(ctx, session, turn, agentSessionTurnRunning, svc.turnStartedStatusMessage(ctx, session, turn.RunID, "", ""), "")
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
	status := normalizeAgentSessionCompleteStatus(command.Status)
	if strings.EqualFold(strings.TrimSpace(command.Artifacts[agentTurnArtifactFailureCode]), agentTurnFailureProviderPolicyBlocked) {
		status = agentSessionTurnBlocked
	}
	currentTurn, err := svc.cfg.Store.GetAgentSessionTurn(ctx, command.TurnID)
	if err != nil {
		return err
	}
	if currentTurn.SessionID != session.ID {
		return fmt.Errorf("turn does not belong to session")
	}
	if agentSessionTurnTerminal(currentTurn.Status) {
		return svc.reconcileCompletedTurnSnapshot(ctx, session, currentTurn, command, currentTurn.Status)
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
	} else if status == agentSessionTurnBlocked {
		sessionStatus = agentSessionStatusBlocked
	}
	if _, err := svc.cfg.Store.UpdateAgentSessionSnapshot(ctx, adminrepo.UpdateAgentSessionSnapshotInput{
		SessionKey:               session.SessionKey,
		CodexSessionID:           command.CodexSessionID,
		SessionArchiveGzipBase64: command.SessionArchiveGzipBase64,
		Status:                   sessionStatus,
		ExtendTTLSeconds:         session.TTLSeconds,
	}); err != nil {
		return err
	}
	if coordinationStore, ok := svc.cfg.Store.(adminrepo.CoordinationRepository); ok {
		_, _ = coordinationStore.UpdateWorkClaim(ctx, adminrepo.UpdateWorkClaimInput{
			TurnID: turn.ID,
			Status: status,
		})
	}
	prURL := ""
	if command.Artifacts != nil {
		prURL = command.Artifacts["pr-url"]
	}
	_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: turn.RunID, Status: status, PRURL: prURL})
	_, _ = svc.upsertTurnStatusCard(ctx, session, turn, status, svc.turnCompletionStatusMessage(ctx, session, status, turn.RunID, command.Artifacts), command.Artifacts["codex-limits"])
	if !capacityRetriesExhausted(command.Artifacts) {
		if err := svc.postTurnResult(ctx, session, turn, status, command); err != nil {
			return err
		}
	}
	if status == agentSessionTurnFailed || status == agentSessionTurnBlocked {
		if err := svc.notifyRootInitiatorFailure(ctx, session, turn, command); err != nil {
			return err
		}
	}
	return nil
}

func (svc *AgentSessionService) reconcileCompletedTurnSnapshot(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, command CompleteAgentSessionTurnCommand, status string) error {
	sessionStatus := agentSessionStatusIdle
	if status == agentSessionTurnFailed {
		sessionStatus = agentSessionStatusError
	} else if status == agentSessionTurnBlocked {
		sessionStatus = agentSessionStatusBlocked
	}
	if _, err := svc.cfg.Store.UpdateAgentSessionSnapshot(ctx, adminrepo.UpdateAgentSessionSnapshotInput{
		SessionKey:               session.SessionKey,
		CodexSessionID:           command.CodexSessionID,
		SessionArchiveGzipBase64: command.SessionArchiveGzipBase64,
		Status:                   sessionStatus,
		ExtendTTLSeconds:         session.TTLSeconds,
	}); err != nil {
		return err
	}
	prURL := ""
	if command.Artifacts != nil {
		prURL = command.Artifacts["pr-url"]
	}
	_, _ = svc.cfg.Store.UpdateAgentRunArtifacts(ctx, adminrepo.UpdateAgentRunArtifactsInput{RunID: turn.RunID, Status: status, PRURL: prURL})
	if status == agentSessionTurnFailed || status == agentSessionTurnBlocked {
		return svc.notifyRootInitiatorFailure(ctx, session, turn, command)
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
		_, _ = svc.upsertTurnStatusCard(ctx, session, canceled, agentSessionTurnCanceled, svc.turnStatusMessage(ctx, session, agentSessionTurnCanceled, canceled.RunID, svc.sessionOpenAIAccountName(ctx, session), ""), "")
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

func (svc *AgentSessionService) RetryFailedTurn(ctx context.Context, command RetryAgentSessionTurnCommand) (RetryAgentSessionTurnResult, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return RetryAgentSessionTurnResult{}, fmt.Errorf("storage is not ready")
	}
	if svc.cfg.TurnDispatcher == nil {
		return RetryAgentSessionTurnResult{}, fmt.Errorf("agent turn dispatcher is not configured")
	}
	if command.TurnID <= 0 {
		return RetryAgentSessionTurnResult{}, fmt.Errorf("turn id is required")
	}
	turn, err := svc.cfg.Store.GetAgentSessionTurn(ctx, command.TurnID)
	if err != nil {
		return RetryAgentSessionTurnResult{}, err
	}
	if turn.Status != agentSessionTurnFailed || !turnCapacityRetriesExhausted(turn) {
		return RetryAgentSessionTurnResult{}, fmt.Errorf("turn is not eligible for capacity retry")
	}
	session, err := svc.cfg.Store.GetAgentSessionByID(ctx, turn.SessionID)
	if err != nil {
		return RetryAgentSessionTurnResult{}, err
	}
	if session.ActiveTurnID != 0 {
		return RetryAgentSessionTurnResult{}, fmt.Errorf("session already has an active turn")
	}
	queued, err := svc.cfg.Store.ListQueuedAgentSessionTurns(ctx, session.ID)
	if err != nil {
		return RetryAgentSessionTurnResult{}, err
	}
	if len(queued) > 0 {
		return RetryAgentSessionTurnResult{}, fmt.Errorf("session already has a queued turn")
	}
	retried, err := svc.cfg.TurnDispatcher.RetryAgentTurn(ctx, AgentTurnRetryRequest{
		Session:  session,
		Turn:     turn,
		UserID:   strings.TrimSpace(command.UserID),
		UserName: strings.TrimSpace(command.UserName),
	})
	if err != nil {
		return RetryAgentSessionTurnResult{}, err
	}
	message := svc.t("chat.session.turn.retry.result", map[string]any{"RunID": retried.RunID})
	result := RetryAgentSessionTurnResult{Message: message, RunID: retried.RunID}
	if strings.TrimSpace(command.ChannelID) != "" && strings.TrimSpace(command.PostID) != "" {
		result.Card = &MattermostCard{
			ChannelID: command.ChannelID,
			PostID:    command.PostID,
			Color:     "#1c58d9",
			Title:     svc.t("chat.session.turn.retry.title", nil),
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
	ref, err := svc.postSessionThreadMessageWithProps(ctx, session, rootPostID, agentNoTriggerMessage(message), agentProgressPostProps(session, 0, ""))
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
	ref, err := svc.postTurnProgressUpdate(ctx, session, turn, message)
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
	cardStatus := phase
	if phase == agentSessionTurnRetrying {
		cardStatus = agentSessionTurnRunning
		message = svc.turnCapacityRetryStatusMessage(ctx, session, runID, command)
	}
	if phase == agentSessionTurnFailed || phase == agentSessionTurnSucceeded {
		message = svc.turnStatusMessage(ctx, session, phase, runID, command.OpenAIAccount, command.CodexLimits)
	}
	ref, err := svc.upsertTurnStatusCard(ctx, session, turn, cardStatus, message, command.CodexLimits)
	if err != nil {
		return AgentSessionPostResult{}, err
	}
	return AgentSessionPostResult{SessionKey: session.SessionKey, ChannelID: ref.ChannelID, PostID: ref.PostID}, nil
}

func (svc *AgentSessionService) RequestAgent(ctx context.Context, sessionKey string, token string, target string, message string) (AgentSessionAgentRequest, error) {
	return svc.requestAgent(ctx, sessionKey, token, target, message, entity.CoordinationCapabilityStartAgents, entity.CoordinationActionStart)
}

func (svc *AgentSessionService) RequestSync(ctx context.Context, sessionKey string, token string, target string, message string) (AgentSessionAgentRequest, error) {
	return svc.requestAgent(ctx, sessionKey, token, target, message, entity.CoordinationCapabilityRequestSync, entity.CoordinationActionRequestSync)
}

func (svc *AgentSessionService) requestAgent(ctx context.Context, sessionKey string, token string, target string, message string, capability string, action string) (AgentSessionAgentRequest, error) {
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
	if err := svc.requireCoordinationPermission(ctx, session, capability, action, role.ID); err != nil {
		return AgentSessionAgentRequest{}, err
	}
	svc.ensureRequestedRoleChannelMember(ctx, project, chat, role)
	repositories, err := svc.chatRepositories(ctx, chat)
	if err != nil {
		return AgentSessionAgentRequest{}, err
	}
	rootPostID := strings.TrimSpace(session.MattermostRootPostID)
	if rootPostID == "" {
		return AgentSessionAgentRequest{}, fmt.Errorf("source session is not bound to a Mattermost thread")
	}
	requesterUserName := svc.sessionMattermostUsername(ctx, session)
	targetSessionKey := agentSessionKey(chat.ID, role.ID, agentSessionScopeThreadRole, rootPostID)
	userMessage := delegatedAgentRequestMessage(requesterUserName, role.Name, message)
	if existingTarget, err := svc.cfg.Store.GetAgentSession(ctx, targetSessionKey); err == nil {
		queuedTurns, err := svc.cfg.Store.ListQueuedAgentSessionTurns(ctx, existingTarget.ID)
		if err != nil {
			return AgentSessionAgentRequest{}, err
		}
		queuedTurn, compatible, err := svc.queuedTurnForProcess(ctx, session.ActiveTurnID, queuedTurns)
		if err != nil {
			return AgentSessionAgentRequest{}, err
		}
		if compatible {
			turn, err := svc.cfg.Store.UpdateAgentSessionTurnMessage(ctx, adminrepo.UpdateAgentSessionTurnMessageInput{
				TurnID:  queuedTurn.ID,
				Message: appendDelegatedAgentRequestToQueuedPrompt(queuedTurn.Message, requesterUserName, role.Name, message),
			})
			if err != nil {
				return AgentSessionAgentRequest{}, err
			}
			if session.ActiveTurnID > 0 {
				turn, err = svc.cfg.Store.AddAgentSessionTurnOrigin(ctx, adminrepo.AddAgentSessionTurnOriginInput{
					TurnID:            turn.ID,
					ParentTurnID:      session.ActiveTurnID,
					TriggerPostID:     rootPostID,
					InitiatorUserName: requesterUserName,
				})
				if err != nil {
					return AgentSessionAgentRequest{}, err
				}
			}
			auditPostID := ""
			if ref, err := svc.postAgentRequestAudit(ctx, session, rootPostID, requesterUserName, role.Name, message); err == nil {
				auditPostID = ref.PostID
			}
			return AgentSessionAgentRequest{
				SessionKey:        session.SessionKey,
				RequestedRunID:    turn.RunID,
				RequestedRoleName: role.Name,
				RequestedRoleID:   role.ID,
				TargetSessionKey:  targetSessionKey,
				AuditPostID:       auditPostID,
			}, nil
		}
	} else if !errors.Is(err, adminrepo.ErrNotFound) {
		return AgentSessionAgentRequest{}, err
	}
	queued, err := svc.cfg.TurnDispatcher.EnqueueAgentTurn(ctx, AgentTurnRequest{
		Project:       project,
		Chat:          chat,
		Role:          role,
		Repositories:  repositories,
		UserName:      requesterUserName,
		UserMessage:   userMessage,
		SourcePostID:  rootPostID,
		ReplyRootID:   rootPostID,
		SessionRootID: rootPostID,
		SessionScope:  agentSessionScopeThreadRole,
		TTLSeconds:    defaultThreadSessionTTLSeconds,
		ParentTurnID:  session.ActiveTurnID,
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

func (svc *AgentSessionService) ensureRequestedRoleChannelMember(ctx context.Context, project entity.Project, chat entity.Chat, role entity.AgentRole) {
	if svc.cfg.Store == nil || svc.cfg.RoleBotManager == nil {
		return
	}
	channelID := strings.TrimSpace(chat.MattermostChannelID)
	if channelID == "" {
		return
	}
	identity, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, role.ID)
	if err != nil || strings.TrimSpace(identity.MattermostUserID) == "" {
		return
	}
	_ = svc.cfg.RoleBotManager.EnsureProjectChannelMember(ctx, project.Slug, channelID, identity.MattermostUserID)
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
	} else if status == agentSessionTurnBlocked {
		message = svc.t("chat.session.provider_policy_blocked", nil)
	}
	if message == "" {
		message = svc.t("chat.run.final_empty", nil)
	}
	_, err := svc.postSessionThreadMessageOnly(ctx, session, turn.MattermostChannelID, turn.MattermostRootPostID, message)
	return err
}

func (svc *AgentSessionService) upsertTurnStatusCard(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, status string, message string, codexLimits string) (MattermostPostRef, error) {
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
	card := svc.turnStatusCard(ctx, session, turn, status, channelID, rootPostID, message)
	var ref MattermostPostRef
	var err error
	if strings.TrimSpace(turn.MattermostStatusPostID) == "" {
		ref, err = svc.cfg.ThreadPublisher.PostThreadCard(ctx, card)
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
		card.PostID = turn.MattermostStatusPostID
		ref, err = svc.cfg.ThreadPublisher.UpdateThreadCard(ctx, card)
		if err != nil {
			return MattermostPostRef{}, err
		}
	}
	project, projectErr := svc.cfg.Store.GetProject(ctx, session.ProjectID)
	role, roleErr := svc.cfg.Store.GetAgentRole(ctx, session.RoleID)
	if projectErr == nil && roleErr == nil {
		_, _ = upsertProjectRunCard(ctx, projectRunCardInput{
			Localizer:         svc.cfg.Localizer,
			Store:             svc.cfg.Store,
			Publisher:         svc.cfg.ThreadPublisher,
			MattermostSiteURL: svc.cfg.MattermostSiteURL,
			Project:           project,
			Session:           session,
			Turn:              turn,
			RoleName:          role.Name,
			OpenAIAccountName: defaultString(session.OpenAIAccountName, role.OpenAIAccountName),
			CodexLimits:       codexLimits,
			Status:            status,
		})
	}
	return ref, nil
}

func (svc *AgentSessionService) turnStatusCard(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, status string, channelID string, rootPostID string, message string) MattermostCard {
	card := MattermostCard{
		ChannelID:  channelID,
		RootPostID: rootPostID,
		ActionURL:  svc.cfg.MenuActionURL,
		Message:    "matter-codex agent turn status #notrigger",
		Props:      agentStatusPostProps(session, turn, status),
		Color:      turnStatusColor(status),
		Title:      svc.t("chat.session.status.title", map[string]any{"Agent": svc.sessionMattermostUsername(ctx, session)}),
		Text:       message,
	}
	if status == agentSessionTurnRunning && strings.TrimSpace(svc.cfg.MenuActionURL) != "" {
		card.Actions = []MattermostCardAction{{
			ID:      "stopturn",
			Name:    svc.t("chat.session.turn.stop.action", nil),
			Tooltip: svc.t("chat.session.turn.stop.tooltip", nil),
			Style:   "danger",
			Context: map[string]any{
				"kind":     "agent_turn",
				"action":   "stop_turn",
				"turn_ids": strconv.FormatInt(turn.ID, 10),
			},
		}}
	} else if status == agentSessionTurnFailed && turnCapacityRetriesExhausted(turn) && strings.TrimSpace(svc.cfg.MenuActionURL) != "" {
		card.Actions = []MattermostCardAction{{
			ID:      "retryturn",
			Name:    svc.t("chat.session.turn.retry.action", nil),
			Tooltip: svc.t("chat.session.turn.retry.tooltip", nil),
			Style:   "primary",
			Context: map[string]any{
				"kind":     "agent_turn",
				"action":   "retry_turn",
				"turn_ids": strconv.FormatInt(turn.ID, 10),
			},
		}}
	}
	return card
}

func turnStatusColor(status string) string {
	switch status {
	case agentSessionTurnSucceeded:
		return "#2f8f46"
	case agentSessionTurnBlocked:
		return "#e67e22"
	case agentSessionTurnFailed:
		return "#d24b40"
	case agentSessionTurnCanceled:
		return "#9aa4b2"
	default:
		return "#1c58d9"
	}
}

func agentStatusPostProps(session entity.AgentSession, turn entity.AgentSessionTurn, status string) map[string]any {
	return map[string]any{
		"matter_codex_event": "agent_status",
		"session_key":        session.SessionKey,
		"role_id":            session.RoleID,
		"turn_id":            turn.ID,
		"run_id":             turn.RunID,
		"status":             status,
	}
}

func (svc *AgentSessionService) postTurnProgressUpdate(ctx context.Context, session entity.AgentSession, turn entity.AgentSessionTurn, message string) (MattermostPostRef, error) {
	channelID := defaultString(turn.MattermostChannelID, session.MattermostChannelID)
	rootPostID := defaultString(turn.MattermostRootPostID, session.MattermostRootPostID)
	if channelID == "" || rootPostID == "" {
		return MattermostPostRef{}, fmt.Errorf("session turn is not bound to a Mattermost thread")
	}
	return svc.postSessionThreadMessageOnlyWithProps(ctx, session, channelID, rootPostID, agentNoTriggerMessage(message), agentProgressPostProps(session, turn.ID, turn.RunID))
}

func agentProgressPostProps(session entity.AgentSession, turnID int64, runID string) map[string]any {
	return map[string]any{
		"matter_codex_event": "agent_progress",
		"session_key":        session.SessionKey,
		"role_id":            session.RoleID,
		"turn_id":            turnID,
		"run_id":             runID,
	}
}

func agentNoTriggerMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" || hasMattermostNoTriggerMarker(message) {
		return message
	}
	return message + "\n\n#notrigger"
}

func (svc *AgentSessionService) postSessionThreadMessage(ctx context.Context, session entity.AgentSession, rootPostID string, message string) (MattermostPostRef, error) {
	return svc.postSessionThreadMessageWithProps(ctx, session, rootPostID, message, nil)
}

func (svc *AgentSessionService) postSessionThreadMessageWithProps(ctx context.Context, session entity.AgentSession, rootPostID string, message string, props map[string]any) (MattermostPostRef, error) {
	return svc.postSessionThreadMessageOnlyWithProps(ctx, session, session.MattermostChannelID, rootPostID, message, props)
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
	return svc.postSessionThreadMessageOnlyWithProps(ctx, session, channelID, rootPostID, message, nil)
}

func (svc *AgentSessionService) postSessionThreadMessageOnlyWithProps(ctx context.Context, session entity.AgentSession, channelID string, rootPostID string, message string, props map[string]any) (MattermostPostRef, error) {
	chunks := svc.splitMattermostThreadMessage(ctx, message)
	roleToken, hasRoleToken := svc.sessionRoleMattermostToken(ctx, session)
	var ref MattermostPostRef
	for _, chunk := range chunks {
		input := MattermostThreadPostInput{
			ChannelID:  channelID,
			RootPostID: rootPostID,
			Message:    chunk,
			Props:      props,
		}
		var err error
		if hasRoleToken {
			ref, err = svc.cfg.ThreadPublisher.PostThreadMessageWithToken(ctx, roleToken, input)
			if err == nil {
				continue
			}
		}
		ref, err = svc.cfg.ThreadPublisher.PostThreadMessage(ctx, input)
		if err != nil {
			return MattermostPostRef{}, err
		}
	}
	return ref, nil
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

func delegatedAgentRequestMessage(requesterUserName string, targetRoleName string, message string) string {
	var body strings.Builder
	body.WriteString("# Запрос к агенту через MatterCodex\n\n")
	appendDelegatedAgentRequestSection(&body, requesterUserName, targetRoleName, message)
	body.WriteString("\n\nОбработай этот запрос как отдельную задачу в текущей MatterCodex thread-session. Если нужно вернуть управление инициатору или manager, используй только `mattermost_request_agent`.")
	return strings.TrimSpace(body.String())
}

func appendDelegatedAgentRequestToQueuedPrompt(existingPrompt string, requesterUserName string, targetRoleName string, message string) string {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(existingPrompt))
	body.WriteString("\n\n# Дополнительный запрос к этому же занятому агенту\n\n")
	appendDelegatedAgentRequestSection(&body, requesterUserName, targetRoleName, message)
	body.WriteString("\n\nЭтот запрос был объединен с уже ожидающим turn. Выполни объединенную работу последовательно, сохранив контекст каждого инициатора.")
	return strings.TrimSpace(body.String()) + "\n"
}

func appendDelegatedAgentRequestSection(body *strings.Builder, requesterUserName string, targetRoleName string, message string) {
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
	fence := markdownFence(strings.TrimSpace(message))
	body.WriteString("- Инициатор: ")
	body.WriteString(requester)
	body.WriteString("\n- Целевой агент: ")
	body.WriteString(target)
	body.WriteString("\n\n## Prompt инициатора\n\n")
	body.WriteString(fence)
	body.WriteString("markdown\n")
	body.WriteString(strings.TrimSpace(message))
	body.WriteString("\n")
	body.WriteString(fence)
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
	ref, err := svc.cfg.ThreadPublisher.UpdateThreadMessageWithToken(ctx, secret.Token, input)
	if err == nil {
		return ref, nil
	}
	return svc.cfg.ThreadPublisher.UpdateThreadMessage(ctx, input)
}

func (svc *AgentSessionService) sessionRoleMattermostToken(ctx context.Context, session entity.AgentSession) (string, bool) {
	identity, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, session.RoleID)
	if err != nil || strings.TrimSpace(identity.TokenSecretRef) == "" {
		return "", false
	}
	secret, err := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, identity.TokenSecretRef)
	if err != nil || strings.TrimSpace(secret.Token) == "" {
		return "", false
	}
	return secret.Token, true
}

func (svc *AgentSessionService) turnStartedStatusMessage(ctx context.Context, session entity.AgentSession, runID string, openAIAccount string, codexLimits string) string {
	return svc.turnStatusMessage(ctx, session, agentSessionTurnRunning, runID, defaultString(openAIAccount, svc.sessionOpenAIAccountName(ctx, session)), codexLimits)
}

func (svc *AgentSessionService) turnCompletionStatusMessage(ctx context.Context, session entity.AgentSession, status string, runID string, artifacts map[string]string) string {
	openAIAccount := ""
	codexLimits := ""
	if artifacts != nil {
		openAIAccount = strings.TrimSpace(artifacts["openai-account"])
		codexLimits = strings.TrimSpace(artifacts["codex-limits"])
	}
	if status == agentSessionTurnFailed && capacityRetriesExhausted(artifacts) {
		data := svc.turnStatusMessageData(ctx, session, runID, defaultString(openAIAccount, svc.sessionOpenAIAccountName(ctx, session)), codexLimits)
		data["RetryCount"] = artifacts[agentTurnArtifactCapacityRetryCount]
		return svc.t("chat.session.status.capacity_exhausted", data)
	}
	if status == agentSessionTurnBlocked {
		data := svc.turnStatusMessageData(ctx, session, runID, defaultString(openAIAccount, svc.sessionOpenAIAccountName(ctx, session)), codexLimits)
		return svc.t("chat.session.status.provider_policy_blocked", data)
	}
	return svc.turnStatusMessage(ctx, session, status, runID, defaultString(openAIAccount, svc.sessionOpenAIAccountName(ctx, session)), codexLimits)
}

func (svc *AgentSessionService) turnStatusMessage(ctx context.Context, session entity.AgentSession, status string, runID string, openAIAccount string, codexLimits string) string {
	data := svc.turnStatusMessageData(ctx, session, runID, openAIAccount, codexLimits)
	if status == agentSessionTurnFailed {
		return svc.t("chat.session.status.failed", data)
	}
	if status == agentSessionTurnCanceled {
		return svc.t("chat.session.status.canceled", data)
	}
	if status == agentSessionTurnBlocked {
		return svc.t("chat.session.status.provider_policy_blocked", data)
	}
	if status == agentSessionTurnRunning {
		return svc.t("chat.session.status.started", data)
	}
	return svc.t("chat.session.status.succeeded", data)
}

func (svc *AgentSessionService) turnStatusMessageData(ctx context.Context, session entity.AgentSession, runID string, openAIAccount string, codexLimits string) map[string]any {
	data := map[string]any{"RunID": runID}
	if agent := strings.TrimSpace(svc.sessionMattermostUsername(ctx, session)); agent != "" {
		data["Agent"] = agent
	}
	if strings.TrimSpace(openAIAccount) != "" {
		data["OpenAIAccount"] = strings.TrimSpace(openAIAccount)
	}
	if strings.TrimSpace(codexLimits) != "" {
		data["CodexLimits"] = strings.TrimSpace(codexLimits)
	}
	return data
}

func (svc *AgentSessionService) turnCapacityRetryStatusMessage(ctx context.Context, session entity.AgentSession, runID string, command UpdateAgentSessionTurnStatusCommand) string {
	data := svc.turnStatusMessageData(ctx, session, runID, command.OpenAIAccount, command.CodexLimits)
	data["Attempt"] = command.RetryAttempt
	data["MaxAttempts"] = command.RetryMaxAttempts
	delayMinutes := command.RetryDelaySeconds / 60
	if delayMinutes <= 0 {
		delayMinutes = 1
	}
	data["DelayMinutes"] = delayMinutes
	return svc.t("chat.session.status.capacity_retry", data)
}

func capacityRetriesExhausted(artifacts map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(artifacts[agentTurnArtifactCapacityRetriesExhausted]), "true")
}

func turnCapacityRetriesExhausted(turn entity.AgentSessionTurn) bool {
	if strings.TrimSpace(turn.Artifacts) == "" {
		return false
	}
	artifacts := map[string]string{}
	if err := json.Unmarshal([]byte(turn.Artifacts), &artifacts); err != nil {
		return false
	}
	return capacityRetriesExhausted(artifacts)
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

func agentSessionTurnTerminal(status string) bool {
	return status == agentSessionTurnSucceeded || status == agentSessionTurnFailed || status == agentSessionTurnCanceled || status == agentSessionTurnBlocked
}

func normalizeAgentSessionCompleteStatus(status string) string {
	if status == agentSessionTurnBlocked {
		return agentSessionTurnBlocked
	}
	if status == agentSessionTurnFailed {
		return agentSessionTurnFailed
	}
	return agentSessionTurnSucceeded
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

func (svc *AgentSessionService) splitMattermostThreadMessage(ctx context.Context, message string) []string {
	limit := svc.mattermostPostMessageMaxRunes(ctx)
	message = strings.TrimSpace(message)
	if message == "" {
		return []string{message}
	}
	if len([]rune(message)) <= limit {
		return []string{message}
	}
	return splitMattermostMessage(message, limit, func(part int, total int) string {
		if svc.cfg.Localizer == nil {
			return fmt.Sprintf("Part %d/%d", part, total)
		}
		return svc.t("chat.session.message_chunk.header", map[string]any{"Part": part, "Total": total})
	})
}

func (svc *AgentSessionService) mattermostPostMessageMaxRunes(ctx context.Context) int {
	limit := defaultMattermostPostMessageMaxRunes
	if reader, ok := svc.cfg.Store.(mattermostPostLimitReader); ok {
		if value, err := reader.MattermostPostMessageMaxRunes(ctx); err == nil && value > 0 {
			limit = value
		}
	}
	if limit < minMattermostPostChunkRunes {
		return minMattermostPostChunkRunes
	}
	return limit
}

func splitMattermostMessage(message string, limit int, header func(part int, total int) string) []string {
	message = strings.TrimSpace(message)
	if message == "" {
		return []string{message}
	}
	if limit < minMattermostPostChunkRunes {
		limit = minMattermostPostChunkRunes
	}
	if len([]rune(message)) <= limit {
		return []string{message}
	}
	payloadLimit := limit - mattermostPostChunkReserveRunes
	if payloadLimit < minMattermostPostChunkRunes {
		payloadLimit = limit
	}
	rawChunks := splitMattermostMessageBody(message, payloadLimit)
	if len(rawChunks) <= 1 {
		return rawChunks
	}
	chunks := make([]string, 0, len(rawChunks))
	for index, chunk := range rawChunks {
		prefix := header(index+1, len(rawChunks))
		bodyLimit := limit - len([]rune(prefix)) - 2
		if bodyLimit < minMattermostPostChunkRunes {
			bodyLimit = payloadLimit
		}
		for _, bodyChunk := range splitMattermostMessageBody(chunk, bodyLimit) {
			chunks = append(chunks, strings.TrimSpace(prefix+"\n\n"+bodyChunk))
		}
	}
	return chunks
}

func splitMattermostMessageBody(message string, limit int) []string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) <= limit {
		return []string{message}
	}
	chunks := make([]string, 0, len(runes)/limit+1)
	for len(runes) > limit {
		cut := preferredMattermostChunkCut(runes, limit)
		chunk := strings.TrimSpace(string(runes[:cut]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		runes = []rune(strings.TrimSpace(string(runes[cut:])))
	}
	if len(runes) > 0 {
		chunks = append(chunks, strings.TrimSpace(string(runes)))
	}
	return chunks
}

func preferredMattermostChunkCut(runes []rune, limit int) int {
	if len(runes) <= limit {
		return len(runes)
	}
	minCut := limit / 2
	for index := limit; index > minCut; index-- {
		if runes[index-1] == '\n' {
			return index
		}
	}
	return limit
}
