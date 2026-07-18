package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	agentDelegationStatusCreating = "creating"
	agentDelegationStatusFailed   = "failed"
)

type AgentSessionChatSummary struct {
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	ChatType      string   `json:"chat_type"`
	SystemPurpose string   `json:"system_purpose,omitempty"`
	Repositories  []string `json:"repositories,omitempty"`
}

type AgentSessionChatCatalog struct {
	SessionKey  string                    `json:"session_key"`
	TargetAgent string                    `json:"target_agent,omitempty"`
	Chats       []AgentSessionChatSummary `json:"chats"`
}

type AgentSessionChatDetails struct {
	SessionKey    string   `json:"session_key"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	ChatType      string   `json:"chat_type"`
	SystemPurpose string   `json:"system_purpose,omitempty"`
	WorkPolicy    string   `json:"work_policy,omitempty"`
	Repositories  []string `json:"repositories,omitempty"`
	Agents        []string `json:"agents"`
}

type StartAgentThreadCommand struct {
	TargetChat  string
	TargetAgent string
	Title       string
	Message     string
	WorkItemKey string
}

type AgentSessionDelegationResult struct {
	DelegationID    int64  `json:"delegation_id"`
	WorkItemKey     string `json:"work_item_key"`
	Status          string `json:"status"`
	TargetChat      string `json:"target_chat"`
	TargetAgent     string `json:"target_agent"`
	TargetThreadURL string `json:"target_thread_url,omitempty"`
	TargetRunID     string `json:"target_run_id,omitempty"`
	CallbackRunID   string `json:"callback_run_id,omitempty"`
}

type AgentSessionDelegationList struct {
	SessionKey  string                         `json:"session_key"`
	Delegations []AgentSessionDelegationResult `json:"delegations"`
}

func (svc *AgentSessionService) ListAvailableChats(ctx context.Context, sessionKey string, token string, targetAgent string) (AgentSessionChatCatalog, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionChatCatalog{}, err
	}
	targetAgent = strings.TrimSpace(strings.TrimPrefix(targetAgent, "@"))
	var targetRole entity.AgentRole
	if targetAgent != "" {
		targetRole, err = svc.resolveRequestedRole(ctx, session.ProjectID, targetAgent)
		if err != nil {
			return AgentSessionChatCatalog{}, err
		}
		targetAgent = targetRole.Name
		if !targetRole.Enabled {
			return AgentSessionChatCatalog{}, fmt.Errorf("agent role %q is disabled", targetRole.Name)
		}
	}

	chats, err := svc.cfg.Store.ListChats(ctx, session.ProjectID)
	if err != nil {
		return AgentSessionChatCatalog{}, err
	}
	result := AgentSessionChatCatalog{SessionKey: session.SessionKey, TargetAgent: targetAgent, Chats: make([]AgentSessionChatSummary, 0, len(chats))}
	for _, chat := range chats {
		participants, err := svc.cfg.Store.ListChatParticipants(ctx, chat.ID)
		if err != nil {
			return AgentSessionChatCatalog{}, err
		}
		if targetRole.ID != 0 && !chatParticipantEnabled(participants, targetRole.ID) {
			continue
		}
		repositories, err := svc.chatRepositories(ctx, chat)
		if err != nil {
			return AgentSessionChatCatalog{}, err
		}
		result.Chats = append(result.Chats, AgentSessionChatSummary{
			Slug:          chat.Slug,
			Name:          chat.Name,
			Description:   chat.Description,
			ChatType:      chat.ChatType,
			SystemPurpose: chat.SystemPurpose,
			Repositories:  projectRepositoryNames(repositories),
		})
	}
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.chat_catalog.side_effect", func(current entity.AgentSession) error {
		result.SessionKey = current.SessionKey
		return nil
	})
	return result, err
}

func (svc *AgentSessionService) ChatDetails(ctx context.Context, sessionKey string, token string, chatKey string) (AgentSessionChatDetails, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionChatDetails{}, err
	}
	chat, err := svc.resolveProjectChat(ctx, session.ProjectID, chatKey)
	if err != nil {
		return AgentSessionChatDetails{}, err
	}
	participants, err := svc.cfg.Store.ListChatParticipants(ctx, chat.ID)
	if err != nil {
		return AgentSessionChatDetails{}, err
	}
	repositories, err := svc.chatRepositories(ctx, chat)
	if err != nil {
		return AgentSessionChatDetails{}, err
	}
	agents := make([]string, 0, len(participants))
	for _, participant := range participants {
		if participant.Enabled {
			agents = append(agents, participant.RoleName)
		}
	}
	result := AgentSessionChatDetails{
		SessionKey:    session.SessionKey,
		Slug:          chat.Slug,
		Name:          chat.Name,
		Description:   chat.Description,
		ChatType:      chat.ChatType,
		SystemPurpose: chat.SystemPurpose,
		WorkPolicy:    chat.WorkPolicy,
		Repositories:  projectRepositoryNames(repositories),
		Agents:        agents,
	}
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.chat_details.side_effect", func(current entity.AgentSession) error {
		result.SessionKey = current.SessionKey
		return nil
	})
	return result, err
}

func (svc *AgentSessionService) StartAgentThread(ctx context.Context, sessionKey string, token string, command StartAgentThreadCommand) (AgentSessionDelegationResult, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	if svc.cfg.TurnDispatcher == nil || svc.cfg.ThreadPublisher == nil {
		return AgentSessionDelegationResult{}, fmt.Errorf("agent thread delegation is not configured")
	}
	command.TargetChat = strings.TrimSpace(command.TargetChat)
	command.TargetAgent = strings.TrimSpace(strings.TrimPrefix(command.TargetAgent, "@"))
	command.Title = strings.TrimSpace(command.Title)
	command.Message = strings.TrimSpace(command.Message)
	command.WorkItemKey = strings.TrimSpace(command.WorkItemKey)
	if command.TargetChat == "" || command.TargetAgent == "" || command.Title == "" || command.Message == "" || command.WorkItemKey == "" {
		return AgentSessionDelegationResult{}, fmt.Errorf("target chat, target agent, title, message, and work item key are required")
	}
	if len(command.WorkItemKey) > 200 || strings.ContainsAny(command.WorkItemKey, "\r\n") {
		return AgentSessionDelegationResult{}, fmt.Errorf("work item key must be a single line no longer than 200 characters")
	}
	if session.ActiveTurnID == 0 {
		return AgentSessionDelegationResult{}, fmt.Errorf("source session has no active turn")
	}
	project, err := svc.cfg.Store.GetProject(ctx, session.ProjectID)
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	targetRole, err := svc.resolveRequestedRole(ctx, session.ProjectID, command.TargetAgent)
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	if !targetRole.Enabled {
		return AgentSessionDelegationResult{}, fmt.Errorf("agent role %q is disabled", targetRole.Name)
	}
	if err := svc.requireCoordinationPermission(ctx, session, entity.CoordinationCapabilityStartAgents, entity.CoordinationActionStart, targetRole.ID); err != nil {
		return AgentSessionDelegationResult{}, err
	}
	targetChat, err := svc.resolveProjectChat(ctx, session.ProjectID, command.TargetChat)
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	participants, err := svc.cfg.Store.ListChatParticipants(ctx, targetChat.ID)
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	if !chatParticipantEnabled(participants, targetRole.ID) {
		return AgentSessionDelegationResult{}, fmt.Errorf("agent %q is not available in chat %q", targetRole.Name, targetChat.Slug)
	}
	if strings.EqualFold(strings.TrimSpace(targetRole.KubernetesAccess), "cluster-admin") {
		return AgentSessionDelegationResult{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	if strings.TrimSpace(targetChat.MattermostChannelID) == "" {
		return AgentSessionDelegationResult{}, fmt.Errorf("chat %q is not bound to a Mattermost channel", targetChat.Slug)
	}

	var delegation entity.AgentDelegation
	var created bool
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_create.side_effect", func(current entity.AgentSession, guardedStore adminrepo.Repository) error {
		var createErr error
		delegation, created, createErr = guardedStore.CreateAgentDelegation(ctx, adminrepo.CreateAgentDelegationInput{
			ProjectID: current.ProjectID, SourceSessionID: current.ID, SourceTurnID: current.ActiveTurnID,
			TargetChatID: targetChat.ID, TargetRoleID: targetRole.ID, WorkItemKey: command.WorkItemKey, Title: command.Title,
		})
		return createErr
	})
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	if !created {
		if delegation.TargetRunID == "" {
			return AgentSessionDelegationResult{}, fmt.Errorf("delegation %q already exists with status %q", delegation.WorkItemKey, delegation.Status)
		}
		return svc.agentDelegationResult(ctx, project, targetChat, targetRole, delegation), nil
	}

	requesterUserName := svc.sessionMattermostUsername(ctx, session)
	if err := svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_membership.side_effect", func(entity.AgentSession) error {
		return svc.ensureRequestedRoleChannelMember(ctx, project, targetChat, targetRole, "", requesterUserName)
	}); err != nil {
		return AgentSessionDelegationResult{}, err
	}
	sourceThreadURL := svc.mattermostThreadURL(project.Slug, session.MattermostRootPostID)
	var rootPost MattermostPostRef
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_thread_publish.side_effect", func(entity.AgentSession) error {
		var publishErr error
		rootPost, publishErr = svc.postDelegatedAgentThread(ctx, delegation, targetChat, targetRole, requesterUserName, sourceThreadURL, command.Message)
		return publishErr
	})
	if err != nil {
		_ = svc.setCurrentSessionDelegationFailed(ctx, session, delegation.ID)
		return AgentSessionDelegationResult{}, err
	}
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_root_persist.side_effect", func(_ entity.AgentSession, guardedStore adminrepo.Repository) error {
		var persistErr error
		delegation, persistErr = guardedStore.SetAgentDelegationRoot(ctx, delegation.ID, rootPost.PostID)
		return persistErr
	})
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	targetThreadURL := svc.mattermostThreadURL(project.Slug, rootPost.PostID)
	var auditPost MattermostPostRef
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_audit_publish.side_effect", func(current entity.AgentSession) error {
		var publishErr error
		auditPost, publishErr = svc.postCrossChatDelegationAudit(ctx, current, requesterUserName, targetRole.Name, targetChat.Slug, targetThreadURL, command.Message)
		return publishErr
	})
	if err != nil {
		_ = svc.setCurrentSessionDelegationFailed(ctx, session, delegation.ID)
		return AgentSessionDelegationResult{}, err
	}
	sourceLaunchURL := svc.mattermostThreadURL(project.Slug, auditPost.PostID)
	if err := svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_thread_update.side_effect", func(entity.AgentSession) error {
		return svc.updateDelegatedAgentThreadSourceLink(ctx, delegation, targetChat, targetRole, requesterUserName, rootPost, sourceThreadURL, sourceLaunchURL, command.Message)
	}); err != nil {
		_ = svc.setCurrentSessionDelegationFailed(ctx, session, delegation.ID)
		return AgentSessionDelegationResult{}, err
	}
	repositories, err := svc.chatRepositories(ctx, targetChat)
	if err != nil {
		_ = svc.setCurrentSessionDelegationFailed(ctx, session, delegation.ID)
		return AgentSessionDelegationResult{}, err
	}
	var queued AgentTurnQueued
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_enqueue.side_effect", func(current entity.AgentSession) error {
		var enqueueErr error
		queued, enqueueErr = svc.cfg.TurnDispatcher.EnqueueAgentTurn(ctx, AgentTurnRequest{
			Project: project, Chat: targetChat, Role: targetRole, Repositories: repositories, UserName: requesterUserName,
			UserMessage:  crossChatDelegatedAgentRequestMessage(requesterUserName, targetRole.Name, command.Message),
			SourcePostID: auditPost.PostID, ReplyRootID: rootPost.PostID, SessionRootID: rootPost.PostID,
			SessionScope: agentSessionScopeThreadRole, TTLSeconds: defaultThreadSessionTTLSeconds, ParentTurnID: current.ActiveTurnID,
		})
		return enqueueErr
	})
	if err != nil {
		_ = svc.setCurrentSessionDelegationFailed(ctx, session, delegation.ID)
		return AgentSessionDelegationResult{}, err
	}
	var targetSession entity.AgentSession
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_target_read.side_effect", func(entity.AgentSession) error {
		var readErr error
		targetSession, readErr = svc.cfg.Store.GetAgentSession(ctx, queued.SessionKey)
		return readErr
	})
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_target_persist.side_effect", func(_ entity.AgentSession, guardedStore adminrepo.Repository) error {
		var persistErr error
		delegation, persistErr = guardedStore.SetAgentDelegationTarget(ctx, delegation.ID, targetSession.ID, queued.TurnID, queued.RunID)
		return persistErr
	})
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	result := svc.agentDelegationResult(ctx, project, targetChat, targetRole, delegation)
	return result, nil
}

func (svc *AgentSessionService) setCurrentSessionDelegationFailed(ctx context.Context, session entity.AgentSession, delegationID int64) error {
	return svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_failed_persist.side_effect", func(_ entity.AgentSession, guardedStore adminrepo.Repository) error {
		_, err := guardedStore.SetAgentDelegationFailed(ctx, delegationID)
		return err
	})
}

func (svc *AgentSessionService) ListDelegations(ctx context.Context, sessionKey string, token string, limit int) (AgentSessionDelegationList, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionDelegationList{}, err
	}
	delegations, err := svc.cfg.Store.ListAgentDelegationsBySource(ctx, session.ID, limit)
	if err != nil {
		return AgentSessionDelegationList{}, err
	}
	project, err := svc.cfg.Store.GetProject(ctx, session.ProjectID)
	if err != nil {
		return AgentSessionDelegationList{}, err
	}
	result := AgentSessionDelegationList{SessionKey: session.SessionKey, Delegations: make([]AgentSessionDelegationResult, 0, len(delegations))}
	for _, delegation := range delegations {
		chat, err := svc.cfg.Store.GetChat(ctx, delegation.TargetChatID)
		if err != nil {
			return AgentSessionDelegationList{}, err
		}
		role, err := svc.cfg.Store.GetAgentRole(ctx, delegation.TargetRoleID)
		if err != nil {
			return AgentSessionDelegationList{}, err
		}
		result.Delegations = append(result.Delegations, svc.agentDelegationResult(ctx, project, chat, role, delegation))
	}
	err = svc.withCurrentSessionRuntimeGuard(ctx, session, "agent_session.delegation_list.side_effect", func(current entity.AgentSession) error {
		result.SessionKey = current.SessionKey
		return nil
	})
	return result, err
}

func (svc *AgentSessionService) ReturnToRequester(ctx context.Context, sessionKey string, token string, message string) (AgentSessionDelegationResult, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return AgentSessionDelegationResult{}, fmt.Errorf("callback message is required")
	}
	if svc.cfg.TurnDispatcher == nil || svc.cfg.ThreadPublisher == nil {
		return AgentSessionDelegationResult{}, fmt.Errorf("agent delegation callback is not configured")
	}
	var delegation entity.AgentDelegation
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_callback_lookup.side_effect", func(current entity.AgentSession, guardedStore adminrepo.Repository) error {
		var readErr error
		delegation, readErr = guardedStore.GetAgentDelegationForCallback(ctx, current.ID)
		return readErr
	})
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return AgentSessionDelegationResult{}, fmt.Errorf("current session was not started by a cross-chat delegation")
		}
		return AgentSessionDelegationResult{}, err
	}
	var project entity.Project
	var targetChat entity.Chat
	var targetRole entity.AgentRole
	if delegation.CallbackRunID != "" {
		if err := svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_callback_read.side_effect", func(_ entity.AgentSession, guardedStore adminrepo.Repository) error {
			var readErr error
			project, readErr = guardedStore.GetProject(ctx, delegation.ProjectID)
			if readErr == nil {
				targetChat, readErr = guardedStore.GetChat(ctx, delegation.TargetChatID)
			}
			if readErr == nil {
				targetRole, readErr = guardedStore.GetAgentRole(ctx, delegation.TargetRoleID)
			}
			return readErr
		}); err != nil {
			return AgentSessionDelegationResult{}, err
		}
		return svc.agentDelegationResult(ctx, project, targetChat, targetRole, delegation), nil
	}
	var sourceSession entity.AgentSession
	err = svc.withCurrentSessionPersistenceGuard(ctx, session, "agent_session.delegation_callback_source_lookup.side_effect", func(_ entity.AgentSession, guardedStore adminrepo.Repository) error {
		var readErr error
		sourceSession, readErr = guardedStore.GetAgentSessionByID(ctx, delegation.SourceSessionID)
		return readErr
	})
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	var sourceChat entity.Chat
	var sourceRole entity.AgentRole
	var requesterUserName string
	var callbackPrompt string
	var turn entity.AgentSessionTurn
	var runID string
	newlyQueued := false
	err = svc.withCurrentSessionsPersistenceGuard(ctx, session, sourceSession, "agent_session.delegation_callback_persist.side_effect", func(_ entity.AgentSession, _ entity.AgentSession, guardedStore adminrepo.Repository) error {
		currentChild, readErr := guardedStore.GetAgentSession(ctx, session.SessionKey)
		if readErr != nil || !sameAgentSessionIdentity(currentChild, session) {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		currentSource, readErr := guardedStore.GetAgentSession(ctx, sourceSession.SessionKey)
		if readErr != nil || !sameAgentSessionIdentity(currentSource, sourceSession) {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		currentDelegation, readErr := guardedStore.GetAgentDelegationForCallback(ctx, currentChild.ID)
		if readErr != nil || currentDelegation.ID != delegation.ID || currentDelegation.SourceSessionID != currentSource.ID {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		delegation = currentDelegation
		project, readErr = guardedStore.GetProject(ctx, delegation.ProjectID)
		if readErr != nil || project.ID != currentChild.ProjectID || project.ID != currentSource.ProjectID {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		targetChat, readErr = guardedStore.GetChat(ctx, delegation.TargetChatID)
		if readErr != nil || targetChat.ID != currentChild.ChatID {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		targetRole, readErr = guardedStore.GetAgentRole(ctx, delegation.TargetRoleID)
		if readErr != nil || targetRole.ID != currentChild.RoleID {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		if delegation.CallbackRunID != "" {
			return nil
		}
		sourceChat, readErr = guardedStore.GetChat(ctx, currentSource.ChatID)
		if readErr != nil || sourceChat.ProjectID != currentSource.ProjectID || strings.TrimSpace(sourceChat.MattermostChannelID) != strings.TrimSpace(currentSource.MattermostChannelID) {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		sourceRole, readErr = guardedStore.GetAgentRole(ctx, currentSource.RoleID)
		if readErr != nil || sourceRole.ProjectID != currentSource.ProjectID {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		if readErr = svc.requireCoordinationPermissionWithStore(ctx, guardedStore, currentChild, entity.CoordinationCapabilityReturnCallback, entity.CoordinationActionCallback, sourceRole.ID); readErr != nil {
			return readErr
		}
		requesterUserName = svc.sessionMattermostUsernameWithStore(ctx, guardedStore, currentChild)
		targetThreadURL := svc.mattermostThreadURL(project.Slug, delegation.TargetRootPostID)
		callbackPrompt = crossChatDelegationCallbackMessage(requesterUserName, delegation.Title, targetThreadURL, message)
		turn, runID, readErr = svc.enqueueDelegationCallbackWithStore(ctx, guardedStore, currentSource, project, sourceChat, sourceRole, requesterUserName, callbackPrompt, delegation.TargetTurnID, delegation.TargetRootPostID)
		if readErr != nil {
			return readErr
		}
		var persistErr error
		delegation, persistErr = guardedStore.SetAgentDelegationCallback(ctx, delegation.ID, turn.ID, runID)
		newlyQueued = persistErr == nil
		return persistErr
	})
	if err != nil {
		return AgentSessionDelegationResult{}, err
	}
	if !newlyQueued {
		return svc.agentDelegationResult(ctx, project, targetChat, targetRole, delegation), nil
	}
	targetThreadURL := svc.mattermostThreadURL(project.Slug, delegation.TargetRootPostID)
	sourceThreadURL := svc.mattermostThreadURL(project.Slug, sourceSession.MattermostRootPostID)
	callbackAuditChunks := svc.splitMattermostThreadMessage(ctx, crossChatDelegationCallbackAuditMessage(requesterUserName, delegation.Title, targetThreadURL, message))
	returnAuditChunks := svc.splitMattermostThreadMessage(ctx, crossChatDelegationReturnAuditMessage(requesterUserName, delegation.Title, sourceThreadURL))
	var callbackPublishErr error
	if err := svc.withCurrentSessionsPublishGuard(ctx, session, sourceSession, "agent_session.delegation_callback_publish_final_guard", func(_ entity.AgentSession, currentSource entity.AgentSession) error {
		_, callbackPublishErr = svc.postSystemThreadMessageChunks(ctx, currentSource.MattermostChannelID, currentSource.MattermostRootPostID, callbackAuditChunks, "agent_cross_chat_callback")
		return nil
	}); err != nil {
		return AgentSessionDelegationResult{}, err
	}
	_ = callbackPublishErr
	var returnPublishErr error
	if err := svc.withCurrentSessionsPublishGuard(ctx, session, sourceSession, "agent_session.delegation_return_publish_final_guard", func(currentChild entity.AgentSession, _ entity.AgentSession) error {
		_, returnPublishErr = svc.postSystemThreadMessageChunks(ctx, currentChild.MattermostChannelID, currentChild.MattermostRootPostID, returnAuditChunks, "agent_cross_chat_callback_returned")
		return nil
	}); err != nil {
		return AgentSessionDelegationResult{}, err
	}
	_ = returnPublishErr
	return svc.agentDelegationResult(ctx, project, targetChat, targetRole, delegation), nil
}

func (svc *AgentSessionService) enqueueDelegationCallbackWithStore(ctx context.Context, guardedStore adminrepo.Repository, sourceSession entity.AgentSession, project entity.Project, sourceChat entity.Chat, sourceRole entity.AgentRole, requesterUserName string, message string, parentTurnID int64, triggerPostID string) (entity.AgentSessionTurn, string, error) {
	queuedTurns, err := guardedStore.ListQueuedAgentSessionTurns(ctx, sourceSession.ID)
	if err != nil {
		return entity.AgentSessionTurn{}, "", err
	}
	queuedTurn, compatible, err := svc.queuedTurnForProcessWithStore(ctx, guardedStore, parentTurnID, queuedTurns)
	if err != nil {
		return entity.AgentSessionTurn{}, "", err
	}
	if compatible {
		compatibleTurn, updateErr := guardedStore.UpdateAgentSessionTurnMessage(ctx, adminrepo.UpdateAgentSessionTurnMessageInput{
			TurnID: queuedTurn.ID, Message: appendDelegationCallbackToQueuedPrompt(queuedTurn.Message, message),
		})
		if updateErr == nil && parentTurnID > 0 {
			compatibleTurn, updateErr = guardedStore.AddAgentSessionTurnOrigin(ctx, adminrepo.AddAgentSessionTurnOriginInput{
				TurnID: compatibleTurn.ID, ParentTurnID: parentTurnID, TriggerPostID: triggerPostID, InitiatorUserName: requesterUserName,
			})
		}
		if updateErr != nil {
			return entity.AgentSessionTurn{}, "", updateErr
		}
		return compatibleTurn, compatibleTurn.RunID, nil
	}
	dispatcher, ok := svc.cfg.TurnDispatcher.(TransactionalAgentTurnDispatcher)
	if !ok {
		return entity.AgentSessionTurn{}, "", fmt.Errorf("agent turn dispatcher does not support transactional existing-session enqueue")
	}
	repositories, err := chatRepositoriesWithStore(ctx, guardedStore, sourceChat)
	if err != nil {
		return entity.AgentSessionTurn{}, "", err
	}
	queued, err := dispatcher.EnqueueExistingAgentTurn(ctx, guardedStore, sourceSession, AgentTurnRequest{
		Project: project, Chat: sourceChat, Role: sourceRole, Repositories: repositories,
		UserName: requesterUserName, UserMessage: message, PreparedPrompt: message,
		SourcePostID: triggerPostID, ReplyRootID: sourceSession.MattermostRootPostID,
		SessionRootID: sourceSession.MattermostRootPostID, SessionScope: sourceSession.SessionScope,
		TTLSeconds: sourceSession.TTLSeconds, ParentTurnID: parentTurnID,
	})
	if err != nil {
		return entity.AgentSessionTurn{}, "", err
	}
	turn, err := guardedStore.GetAgentSessionTurn(ctx, queued.TurnID)
	return turn, queued.RunID, err
}

func (svc *AgentSessionService) resolveProjectChat(ctx context.Context, projectID int64, chatKey string) (entity.Chat, error) {
	chatKey = strings.TrimSpace(chatKey)
	if chatKey == "" {
		return entity.Chat{}, fmt.Errorf("chat is required")
	}
	chats, err := svc.cfg.Store.ListChats(ctx, projectID)
	if err != nil {
		return entity.Chat{}, err
	}
	for _, chat := range chats {
		if strings.EqualFold(chat.Slug, chatKey) || strings.EqualFold(chat.Name, chatKey) {
			return chat, nil
		}
	}
	return entity.Chat{}, fmt.Errorf("chat %q was not found in the current project", chatKey)
}

func chatParticipantEnabled(participants []entity.ChatParticipant, roleID int64) bool {
	for _, participant := range participants {
		if participant.RoleID == roleID && participant.Enabled {
			return true
		}
	}
	return false
}

func projectRepositoryNames(repositories []entity.ProjectRepository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		if name := strings.TrimSpace(repository.FullName()); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (svc *AgentSessionService) postDelegatedAgentThread(ctx context.Context, delegation entity.AgentDelegation, chat entity.Chat, role entity.AgentRole, requesterUserName string, sourceThreadURL string, message string) (MattermostPostRef, error) {
	body := crossChatDelegationRootMessage(requesterUserName, role.Name, delegation.Title, sourceThreadURL, message)
	chunks := svc.splitMattermostThreadMessage(ctx, body)
	if len(chunks) == 0 {
		return MattermostPostRef{}, fmt.Errorf("delegation thread message is empty")
	}
	root, err := svc.cfg.ThreadPublisher.PostThreadMessage(ctx, MattermostThreadPostInput{
		ChannelID: chat.MattermostChannelID,
		Message:   agentNoTriggerMessage(chunks[0]),
		Props: map[string]any{
			"matter_codex_event": "agent_thread_delegation",
			"delegation_id":      delegation.ID,
		},
	})
	if err != nil {
		return MattermostPostRef{}, err
	}
	for _, chunk := range chunks[1:] {
		if _, err := svc.cfg.ThreadPublisher.PostThreadMessage(ctx, MattermostThreadPostInput{
			ChannelID:  chat.MattermostChannelID,
			RootPostID: root.PostID,
			Message:    agentNoTriggerMessage(chunk),
			Props: map[string]any{
				"matter_codex_event": "agent_thread_delegation_continuation",
				"delegation_id":      delegation.ID,
			},
		}); err != nil {
			return MattermostPostRef{}, err
		}
	}
	return root, nil
}

func (svc *AgentSessionService) updateDelegatedAgentThreadSourceLink(ctx context.Context, delegation entity.AgentDelegation, chat entity.Chat, role entity.AgentRole, requesterUserName string, rootPost MattermostPostRef, previousURL string, sourceLaunchURL string, message string) error {
	body := crossChatDelegationRootMessage(requesterUserName, role.Name, delegation.Title, previousURL, message)
	chunks := svc.splitMattermostThreadMessage(ctx, body)
	if len(chunks) == 0 || strings.TrimSpace(previousURL) == "" || strings.TrimSpace(sourceLaunchURL) == "" {
		return fmt.Errorf("delegation source launch message link is empty")
	}
	rootMessage := agentNoTriggerMessage(chunks[0])
	updatedMessage := strings.Replace(rootMessage, previousURL, sourceLaunchURL, 1)
	if updatedMessage == rootMessage {
		return fmt.Errorf("delegation source thread link was not found in the target root message")
	}
	_, err := svc.cfg.ThreadPublisher.UpdateThreadMessage(ctx, MattermostThreadUpdateInput{
		ChannelID: chat.MattermostChannelID,
		PostID:    rootPost.PostID,
		Message:   updatedMessage,
		Props: map[string]any{
			"matter_codex_event": "agent_thread_delegation",
			"delegation_id":      delegation.ID,
		},
	})
	return err
}

func (svc *AgentSessionService) postCrossChatDelegationAudit(ctx context.Context, session entity.AgentSession, requester string, targetAgent string, targetChat string, targetURL string, message string) (MattermostPostRef, error) {
	body := crossChatDelegationAuditMessage(requester, targetAgent, targetChat, targetURL, message)
	return svc.postSystemThreadMessage(ctx, session.MattermostChannelID, session.MattermostRootPostID, body, "agent_cross_chat_request")
}

func (svc *AgentSessionService) postDelegationCallbackAudit(ctx context.Context, sourceSession entity.AgentSession, requester string, title string, targetURL string, message string) (MattermostPostRef, error) {
	body := crossChatDelegationCallbackAuditMessage(requester, title, targetURL, message)
	return svc.postSystemThreadMessage(ctx, sourceSession.MattermostChannelID, sourceSession.MattermostRootPostID, body, "agent_cross_chat_callback")
}

func (svc *AgentSessionService) postDelegationReturnAudit(ctx context.Context, targetSession entity.AgentSession, requester string, title string, sourceURL string) (MattermostPostRef, error) {
	body := crossChatDelegationReturnAuditMessage(requester, title, sourceURL)
	return svc.postSystemThreadMessage(ctx, targetSession.MattermostChannelID, targetSession.MattermostRootPostID, body, "agent_cross_chat_callback_returned")
}

func (svc *AgentSessionService) postSystemThreadMessage(ctx context.Context, channelID string, rootPostID string, message string, event string) (MattermostPostRef, error) {
	chunks := svc.splitMattermostThreadMessage(ctx, message)
	return svc.postSystemThreadMessageChunks(ctx, channelID, rootPostID, chunks, event)
}

func (svc *AgentSessionService) postSystemThreadMessageChunks(ctx context.Context, channelID string, rootPostID string, chunks []string, event string) (MattermostPostRef, error) {
	var firstRef MattermostPostRef
	for _, chunk := range chunks {
		ref, err := svc.cfg.ThreadPublisher.PostThreadMessage(ctx, MattermostThreadPostInput{
			ChannelID:  channelID,
			RootPostID: rootPostID,
			Message:    agentNoTriggerMessage(chunk),
			Props: map[string]any{
				"matter_codex_event": event,
			},
		})
		if err != nil {
			return MattermostPostRef{}, err
		}
		if strings.TrimSpace(firstRef.PostID) == "" {
			firstRef = ref
		}
	}
	return firstRef, nil
}

func (svc *AgentSessionService) agentDelegationResult(ctx context.Context, project entity.Project, chat entity.Chat, role entity.AgentRole, delegation entity.AgentDelegation) AgentSessionDelegationResult {
	result := AgentSessionDelegationResult{
		DelegationID:    delegation.ID,
		WorkItemKey:     delegation.WorkItemKey,
		Status:          delegation.Status,
		TargetChat:      chat.Slug,
		TargetAgent:     role.Name,
		TargetThreadURL: svc.mattermostThreadURL(project.Slug, delegation.TargetRootPostID),
		TargetRunID:     delegation.TargetRunID,
		CallbackRunID:   delegation.CallbackRunID,
	}
	return result
}

func (svc *AgentSessionService) mattermostThreadURL(projectSlug string, rootPostID string) string {
	base := strings.TrimRight(strings.TrimSpace(svc.cfg.MattermostSiteURL), "/")
	projectSlug = strings.Trim(strings.TrimSpace(projectSlug), "/")
	rootPostID = strings.TrimSpace(rootPostID)
	if base == "" || projectSlug == "" || rootPostID == "" {
		return ""
	}
	return base + "/" + projectSlug + "/pl/" + rootPostID
}

func crossChatDelegatedAgentRequestMessage(requesterUserName string, targetRoleName string, message string) string {
	var body strings.Builder
	body.WriteString(delegatedAgentRequestMessage(requesterUserName, targetRoleName, message))
	body.WriteString("\n\nЭта задача запущена в отдельном дочернем треде. По завершении обязательно вызови `mattermost_return_to_requester` с самодостаточным итогом; инструмент вернет управление непосредственному инициатору без упоминаний в Mattermost.")
	return body.String()
}

func crossChatDelegationRootMessage(requester string, target string, title string, sourceThreadURL string, message string) string {
	requester = mentionableMattermostUsername(requester)
	if requester != "" {
		requester = "@" + requester
	} else {
		requester = "agent"
	}
	target = strings.TrimPrefix(mentionableMattermostUsername(target), "@")
	fence := markdownFence(message)
	return fmt.Sprintf("## %s\n\n%s запустил @%s.\n\nИсходный тред: %s\n\n%smarkdown\n%s\n%s", title, requester, target, emptyAsUnknown(sourceThreadURL), fence, message, fence)
}

func crossChatDelegationAuditMessage(requester string, target string, chat string, threadURL string, message string) string {
	requester = strings.TrimPrefix(mentionableMattermostUsername(requester), "@")
	target = strings.TrimPrefix(mentionableMattermostUsername(target), "@")
	fence := markdownFence(message)
	return fmt.Sprintf("matter-codex: @%s запустил @%s в ~%s: %s\n\n%smarkdown\n%s\n%s", requester, target, chat, threadURL, fence, message, fence)
}

func crossChatDelegationCallbackMessage(requester string, title string, threadURL string, message string) string {
	requester = strings.TrimPrefix(mentionableMattermostUsername(requester), "@")
	fence := markdownFence(message)
	return fmt.Sprintf("# Обратный вызов из дочернего треда\n\n- Агент: @%s\n- Работа: %s\n- Дочерний тред: %s\n\n%smarkdown\n%s\n%s\n\nПродолжи координацию с учетом этого результата.", requester, title, threadURL, fence, message, fence)
}

func appendDelegationCallbackToQueuedPrompt(existingPrompt string, callback string) string {
	return strings.TrimSpace(existingPrompt) + "\n\n# Дополнительный обратный вызов из дочернего треда\n\n" + strings.TrimSpace(callback) + "\n"
}

func crossChatDelegationCallbackAuditMessage(requester string, title string, threadURL string, message string) string {
	requester = strings.TrimPrefix(mentionableMattermostUsername(requester), "@")
	fence := markdownFence(message)
	return fmt.Sprintf("matter-codex: @%s вернул результат по работе **%s** из %s.\n\n%smarkdown\n%s\n%s", requester, title, threadURL, fence, message, fence)
}

func crossChatDelegationReturnAuditMessage(requester string, title string, sourceThreadURL string) string {
	requester = strings.TrimPrefix(mentionableMattermostUsername(requester), "@")
	return fmt.Sprintf("matter-codex: @%s вернул результат по работе **%s** в исходный тред: %s", requester, title, emptyAsUnknown(sourceThreadURL))
}
