package service

import (
	"context"
	"fmt"
	"io"
	"strings"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

type PublishAgentSessionArtifactCommand struct {
	TurnID         string
	IdempotencyKey string
	OriginalName   string
	Body           io.Reader
	Quarantine     *domainartifact.QuarantineInput
}

func (svc *AgentSessionService) DownloadArtifact(
	ctx context.Context,
	sessionKey string,
	token string,
	turnID string,
	versionID string,
) (domainartifact.Version, io.ReadCloser, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return domainartifact.Version{}, nil, err
	}
	if svc.cfg.Artifacts == nil {
		return domainartifact.Version{}, nil, fmt.Errorf("artifact service is not configured")
	}
	var version domainartifact.Version
	var body io.ReadCloser
	err = svc.withCurrentSessionRuntimeGuardWithStore(ctx, session, "agent_session.artifact_download.side_effect", func(current entity.AgentSession, store adminrepo.Repository) error {
		turn, scope, scopeErr := activeArtifactScope(ctx, store, current, turnID)
		if scopeErr != nil {
			return scopeErr
		}
		if turn.RunID != strings.TrimSpace(turnID) {
			return domainartifact.ErrScopeDenied
		}
		version, body, scopeErr = svc.cfg.Artifacts.OpenForTurn(ctx, scope, strings.TrimSpace(versionID))
		return scopeErr
	})
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		return domainartifact.Version{}, nil, err
	}
	return version, body, nil
}

func (svc *AgentSessionService) PublishArtifact(
	ctx context.Context,
	sessionKey string,
	token string,
	command PublishAgentSessionArtifactCommand,
) (domainartifact.PublishResult, error) {
	session, err := svc.authorize(ctx, sessionKey, token)
	if err != nil {
		return domainartifact.PublishResult{}, err
	}
	if svc.cfg.Artifacts == nil {
		return domainartifact.PublishResult{}, fmt.Errorf("artifact service is not configured")
	}
	var result domainartifact.PublishResult
	err = svc.withCurrentSessionRuntimeGuardWithStore(ctx, session, "agent_session.artifact_publish.side_effect", func(current entity.AgentSession, store adminrepo.Repository) error {
		_, scope, scopeErr := activeArtifactScope(ctx, store, current, command.TurnID)
		if scopeErr != nil {
			return scopeErr
		}
		identity, identityErr := store.GetMattermostBotIdentityByRoleID(ctx, current.RoleID)
		if identityErr != nil || identity.ProjectID != current.ProjectID || strings.TrimSpace(identity.TokenSecretRef) == "" {
			return domainartifact.ErrScopeDenied
		}
		result, scopeErr = svc.cfg.Artifacts.PublishOutgoing(ctx, domainartifact.PublishInput{
			Scope:             scope,
			IdempotencyKey:    strings.TrimSpace(command.IdempotencyKey),
			OriginalName:      command.OriginalName,
			BotTokenSecretRef: strings.TrimSpace(identity.TokenSecretRef),
			Body:              command.Body,
			Quarantine:        command.Quarantine,
		})
		return scopeErr
	})
	return result, err
}

func activeArtifactScope(ctx context.Context, store adminrepo.Repository, session entity.AgentSession, turnID string) (entity.AgentSessionTurn, domainartifact.Scope, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" || session.Status != agentSessionStatusRunning || session.ActiveTurnID <= 0 || strings.TrimSpace(session.ActiveRunID) != turnID {
		return entity.AgentSessionTurn{}, domainartifact.Scope{}, domainartifact.ErrScopeDenied
	}
	turn, err := store.GetAgentSessionTurn(ctx, session.ActiveTurnID)
	if err != nil {
		return entity.AgentSessionTurn{}, domainartifact.Scope{}, err
	}
	if turn.SessionID != session.ID || strings.TrimSpace(turn.RunID) != turnID || turn.Status != agentSessionTurnRunning ||
		strings.TrimSpace(turn.MattermostChannelID) != strings.TrimSpace(session.MattermostChannelID) || strings.TrimSpace(turn.MattermostRootPostID) == "" {
		return entity.AgentSessionTurn{}, domainartifact.Scope{}, domainartifact.ErrScopeDenied
	}
	return turn, artifactScope(session, turn), nil
}

func artifactScope(session entity.AgentSession, turn entity.AgentSessionTurn) domainartifact.Scope {
	return domainartifact.Scope{
		ProjectID:            session.ProjectID,
		ChatID:               session.ChatID,
		SessionID:            session.ID,
		RoleID:               session.RoleID,
		RuntimeTurnID:        turn.ID,
		TurnID:               turn.RunID,
		SessionKey:           session.SessionKey,
		MattermostChannelID:  turn.MattermostChannelID,
		MattermostRootPostID: turn.MattermostRootPostID,
	}
}
