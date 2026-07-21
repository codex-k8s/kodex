package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	integrationsdomain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
)

var _ integrationsdomain.SessionAdmission = (*AgentSessionService)(nil)

// AuthorizeIntegrationSession усиливает общий bearer admission точным turn и human initiator binding.
func (svc *AgentSessionService) AuthorizeIntegrationSession(ctx context.Context, sessionKey string, token string) (integrationsdomain.SessionContext, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	now := time.Now().UTC()
	if session.Status != agentSessionStatusRunning || session.ActiveTurnID <= 0 || !session.ExpiresAt.After(now) ||
		strings.TrimSpace(session.MattermostChannelID) == "" || strings.TrimSpace(session.MattermostRootPostID) == "" ||
		strings.TrimSpace(session.TokenSecretRef) == "" {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	mutationGuard, ok := svc.cfg.Store.(securityrepo.IntegrationMutationPathRepository)
	if !ok {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	directMutationPath, err := mutationGuard.HasDirectIntegrationMutationPath(ctx, session.RoleID, session.SessionKey)
	if err != nil || directMutationPath {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	turn, err := svc.cfg.Store.GetAgentSessionTurn(ctx, session.ActiveTurnID)
	if err != nil || turn.SessionID != session.ID || turn.Status != agentSessionTurnRunning ||
		turn.MattermostChannelID != session.MattermostChannelID || turn.MattermostRootPostID != session.MattermostRootPostID {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	approverUserID, err := svc.rootInitiatorUserIDForTurn(ctx, svc.cfg.Store, turn.ID)
	if err != nil {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	approverUserName := strings.TrimSpace(turn.UserName)
	if coordination, ok := svc.cfg.Store.(adminrepo.CoordinationRepository); ok {
		process, processErr := coordination.GetTurnProcess(ctx, turn.ID)
		if processErr == nil {
			approverUserName = strings.TrimSpace(process.RootInitiatorUserName)
		} else if !errors.Is(processErr, adminrepo.ErrNotFound) {
			return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
		}
	}
	if _, identityErr := svc.cfg.Store.GetMattermostBotIdentityByUserID(ctx, approverUserID); identityErr == nil {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	} else if !errors.Is(identityErr, adminrepo.ErrNotFound) {
		return integrationsdomain.SessionContext{}, integrationsdomain.ErrUnauthorized
	}
	return integrationsdomain.SessionContext{
		SessionID: session.ID, SessionKey: session.SessionKey, TurnID: turn.ID,
		ProjectID: session.ProjectID, ChatID: session.ChatID, RoleID: session.RoleID,
		SubjectKind: integrationsdomain.SubjectKindAgentRole, SubjectRef: strconv.FormatInt(session.RoleID, 10),
		InstallationScope: integrationsdomain.InstallationScope, WorkspaceScope: strconv.FormatInt(session.ProjectID, 10),
		MattermostChannelID: session.MattermostChannelID, MattermostRootPostID: session.MattermostRootPostID,
		ApproverUserID: approverUserID, ApproverUserName: approverUserName,
		SessionTokenSecretRef: session.TokenSecretRef,
	}, nil
}
