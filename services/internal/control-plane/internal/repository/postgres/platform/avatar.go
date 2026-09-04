package platform

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const avatarContentPathPrefix = "/api/v1/artifacts/"

func parseAvatarArtifactURL(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return "", "", errs.ErrInvalid
	}
	if !strings.HasPrefix(parsed.Path, avatarContentPathPrefix) || !strings.HasSuffix(parsed.Path, "/content") {
		return "", "", errs.ErrInvalid
	}
	artifactRef := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, avatarContentPathPrefix), "/content")
	if artifactRef == "" || strings.Contains(artifactRef, "/") {
		return "", "", errs.ErrInvalid
	}
	query := parsed.Query()
	if len(query) != 1 || len(query["purpose"]) != 1 || query.Get("purpose") != "PREVIEW" {
		return "", "", errs.ErrInvalid
	}
	canonical := avatarArtifactContentURL(artifactRef)
	if value != canonical {
		return "", "", errs.ErrInvalid
	}
	return canonical, artifactRef, nil
}

func avatarArtifactContentURL(artifactRef string) string {
	return avatarContentPathPrefix + artifactRef + "/content?purpose=PREVIEW"
}

func setAgentAvatarReadback(agent *entity.Agent) {
	agent.Avatar.Source = "FALLBACK"
	agent.Avatar.ContentPath = ""
	if agent.Avatar.ArtifactRef != "" {
		agent.Avatar.Source = "ARTIFACT"
		agent.Avatar.ContentPath = avatarArtifactContentURL(agent.Avatar.ArtifactRef)
	}
}

func (repository *Repository) validateAvatarArtifact(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectID string,
	value string,
) (string, error) {
	canonical, artifactRef, err := parseAvatarArtifactURL(value)
	if err != nil || canonical == "" {
		return canonical, err
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "artifact.bind", "ARTIFACT", artifactRef, "")
	if err != nil {
		return "", errs.ErrInvalid
	}
	if err := repository.requireAccess(ctx, tx, current, "artifact.bind", target); err != nil {
		return "", errs.ErrNotFound
	}
	var storedRef string
	err = tx.QueryRow(ctx, queryCommandsValidateAvatarArtifact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_id":      projectID,
		"artifact_ref":    artifactRef,
	}).Scan(&storedRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrInvalid
	}
	if err != nil {
		return "", errs.ErrUnavailable
	}
	if storedRef != artifactRef {
		return "", errs.ErrConflict
	}
	return canonical, nil
}

func (repository *Repository) validateAvatarUpdate(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectID string,
	currentValue string,
	nextValue string,
) (string, error) {
	if nextValue == currentValue {
		canonical, _, err := parseAvatarArtifactURL(nextValue)
		return canonical, err
	}
	return repository.validateAvatarArtifact(ctx, tx, current, projectID, nextValue)
}

func (repository *Repository) syncAgentAvatar(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	agentRef string,
	artifactRef string,
) (entity.AgentAvatar, error) {
	var avatar entity.AgentAvatar
	err := tx.QueryRow(ctx, queryCommandsSyncAgentAvatar, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"agent_ref":       agentRef,
		"artifact_ref":    artifactRef,
		"actor_id":        current.actorID,
	}).Scan(&avatar.ArtifactRef, &avatar.ArtifactRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentAvatar{}, errs.ErrConflict
	}
	if err != nil {
		return entity.AgentAvatar{}, errs.ErrUnavailable
	}
	avatar.Source = "FALLBACK"
	if avatar.ArtifactRef != "" {
		avatar.Source = "ARTIFACT"
		avatar.ContentPath = avatarArtifactContentURL(avatar.ArtifactRef)
	}
	return avatar, nil
}

func (repository *Repository) changeAgentAvatar(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	input command.Command,
) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentAvatarInput)
	if !ok || payload.AgentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, current, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if agent.projectID == "" {
		return commandOutcome{}, errs.ErrProtected
	}
	if agent.agentVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	avatarURL, artifactRef := "", ""
	switch input.Kind {
	case command.SetAgentAvatar:
		if payload.ArtifactRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		avatarURL, artifactRef = avatarArtifactContentURL(payload.ArtifactRef), payload.ArtifactRef
		if _, err := repository.validateAvatarArtifact(ctx, tx, current, agent.projectID, avatarURL); err != nil {
			return commandOutcome{}, err
		}
	case command.RemoveAgentAvatar:
		if payload.ArtifactRef != "" {
			return commandOutcome{}, errs.ErrInvalid
		}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	var item entity.Agent
	if err := tx.QueryRow(ctx, queryCommandsChangeAgentAvatarURL, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": payload.AgentRef,
		"expected_version": agent.agentVersion, "avatar_url": avatarURL,
	}).Scan(&agent.projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription,
		&item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.Avatar, err = repository.syncAgentAvatar(ctx, tx, current, item.Ref, artifactRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := tx.QueryRow(ctx, queryCommandsChangeagentSelectAgentsRef, item.Ref).Scan(
		&item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName,
		&item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model,
		&item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs,
		&item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision,
	); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	setAgentAvatarReadback(&item)
	item.NextActions = agentActions(item, true, true)
	return commandOutcome{
		result: command.Result{Agent: &item}, projectID: agent.projectID, projectRef: item.ProjectRef,
		resourceKind: "AGENT", resourceRef: item.Ref, summary: "i18n:AGENT_AVATAR_UPDATED",
		platformEvent: "AGENT_CHANGED",
	}, nil
}
