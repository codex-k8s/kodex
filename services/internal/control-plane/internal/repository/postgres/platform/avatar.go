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

// avatarContentPathPrefix pins the same-origin artifact content endpoint prefix used by canonical
// avatar URLs. The prefix is routing syntax and conveys no artifact ownership or download
// authority.
// avatarContentPathPrefix фиксирует same-origin artifact content endpoint prefix для canonical
// avatar URLs. Prefix задаёт routing syntax и не выдаёт artifact ownership либо download
// authority.
const avatarContentPathPrefix = "/api/v1/artifacts/"

// parseAvatarArtifactURL trims input and treats empty as clearing the avatar; otherwise accepts
// only the exact relative artifact content URL with one purpose=PREVIEW and canonical bytes.
// Absolute/host/user/fragment, nested refs and alternate query forms fail Invalid. It parses a
// locator without checking existence, scan or permission.
// parseAvatarArtifactURL обрезает пробелы и считает empty удалением avatar; иначе принимает только
// exact relative artifact content URL с единственным purpose=PREVIEW и canonical bytes.
// Absolute/host/user/fragment, nested refs и alternative query forms дают Invalid. Разбирает
// locator без проверки existence, scan либо permission.
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

// avatarArtifactContentURL concatenates the canonical artifact PREVIEW route from the supplied ref
// without validation or escaping. Callers must validate ref/input and actual artifact eligibility
// before exposing or binding it.
// avatarArtifactContentURL собирает canonical artifact PREVIEW route из переданного ref без
// validation либо escaping. Caller проверяет ref/input и actual artifact eligibility до выдачи или
// binding.
func avatarArtifactContentURL(artifactRef string) string {
	return avatarContentPathPrefix + artifactRef + "/content?purpose=PREVIEW"
}

// setAgentAvatarReadback resets avatar presentation to FALLBACK/empty content path, then selects
// ARTIFACT/canonical preview path when the stored artifact ref is nonempty. It mutates a nonnil
// agent but performs no artifact access or content lookup.
// setAgentAvatarReadback сбрасывает avatar presentation в FALLBACK/empty content path, затем
// выбирает ARTIFACT/canonical preview path при nonempty stored artifact ref. Меняет nonnil agent
// без artifact access либо content lookup.
func setAgentAvatarReadback(agent *entity.Agent) {
	agent.Avatar.Source = "FALLBACK"
	agent.Avatar.ContentPath = ""
	if agent.Avatar.ArtifactRef != "" {
		agent.Avatar.Source = "ARTIFACT"
		agent.Avatar.ContentPath = avatarArtifactContentURL(agent.Avatar.ArtifactRef)
	}
}

// validateAvatarArtifact parses the canonical locator, resolves artifact.bind and checks access
// before SQL requires same-project active CLEAN previewable bounded image under head SHARE lock.
// Target-resolution errors become Invalid, permission failure NotFound, SQL absence Invalid and
// mismatched ref Conflict. Empty input returns without lookup; no binding or commit occurs.
// validateAvatarArtifact разбирает canonical locator, разрешает artifact.bind и проверяет access
// до SQL требования same-project active CLEAN previewable bounded image под head SHARE lock.
// Target-resolution errors дают Invalid, permission failure — NotFound, SQL absence — Invalid,
// mismatched ref — Conflict. Empty input возвращается без lookup; binding и commit отсутствуют.
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

// validateAvatarUpdate for byte-identical old/new URL validates only canonical syntax, otherwise
// runs full current artifact admission. Keeping an existing value therefore does not rebind it or
// claim a fresh scan/permission check.
// validateAvatarUpdate для byte-identical old/new URL проверяет только canonical syntax, иначе
// запускает full current artifact admission. Сохранение existing value поэтому не выполняет
// rebinding и не доказывает fresh scan/permission check.
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

// syncAgentAvatar runs the tenant/agent owner SQL to replace avatar binding, retire an
// unreferenced old avatar and project exact artifact ref/revision. Missing candidate/update is
// Conflict, backend failure Unavailable; it derives fallback/artifact presentation without
// committing or independently checking actor permissions.
// syncAgentAvatar выполняет tenant/agent owner SQL для замены avatar binding, retirement старого
// avatar без других ссылок и проекции exact artifact ref/revision. Missing candidate/update даёт
// Conflict, backend failure — Unavailable; выводит fallback/artifact presentation без commit либо
// самостоятельной actor permission check.
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

// changeAgentAvatar requires typed input/version, locks the agent, rejects protected nonproject
// agents and checks OCC before specialized set/remove validation. It updates URL, synchronizes
// binding/revision and rereads the full agent projection, returning AGENT_CHANGED intent to the
// enclosing owner transaction. No external upload or commit occurs; any later failure requires
// caller rollback.
// changeAgentAvatar требует typed input/version, блокирует agent, отклоняет protected nonproject
// agents и проверяет OCC до specialized set/remove validation. Обновляет URL, синхронизирует
// binding/revision и перечитывает full agent projection, возвращая AGENT_CHANGED intent enclosing
// owner transaction. External upload и commit отсутствуют; поздний отказ требует caller rollback.
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
