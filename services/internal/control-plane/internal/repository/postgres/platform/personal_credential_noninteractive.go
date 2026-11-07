package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPersonalNoninteractiveAgentGrant resolves one already-authorized grant to its exact
// published package and capability without accepting a client-selected operation or source.
// queryPersonalNoninteractiveAgentGrant разрешает один уже авторизованный grant в точный
// опубликованный пакет и capability, не принимая operation либо source из клиентского payload.
//
//go:embed sql/personal_noninteractive__agent_grant.sql
var queryPersonalNoninteractiveAgentGrant string

// requireNoninteractiveAgentGrantCredentials protects the agent-card enable command as well as
// the Connection command. Package semantics decide whether the exact grant is an inbound source;
// ordinary integration grants do not become noninteractive simply because they use a personal key.
// requireNoninteractiveAgentGrantCredentials защищает включение через карточку сотрудника наряду
// с командой Connection. Входящий source определяется семантикой точного пакета; обычный grant
// не становится неинтерактивным лишь из-за использования личного ключа.
func (repository *Repository) requireNoninteractiveAgentGrantCredentials(ctx context.Context, tx pgx.Tx, current scope, projectID, agentRef, grantRef string) error {
	var connectionRef, definitionKey, definitionVersion, definitionDigest, capabilityKey string
	if err := tx.QueryRow(ctx, queryPersonalNoninteractiveAgentGrant, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": agentRef, "grant_ref": grantRef,
	}).Scan(&connectionRef, &definitionKey, &definitionVersion, &definitionDigest, &capabilityKey); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connectionRef, definitionKey, definitionVersion, definitionDigest)
	if err != nil {
		return err
	}
	capability, ok := definition.Capability(capabilityKey)
	if !ok {
		return errs.ErrConflict
	}
	if capability.Operation != "mattermost.inbound" {
		return nil
	}
	return repository.requireNoninteractiveTargetCredentials(ctx, tx, current, projectID, entity.RunTarget{Type: "AGENT", Ref: agentRef}, "INTEGRATION", "")
}

// requireNoninteractiveTargetCredentials resolves the current published target before a schedule
// or inbound route is saved or enabled. The configuration author is not an interactive root
// initiator, even when their own key is READY. Shared-only targets retain their existing path;
// later launch and actual-call checks independently revalidate mutable grants and credentials.
// requireNoninteractiveTargetCredentials разрешает текущий опубликованный target до сохранения
// или включения расписания либо входящего маршрута. Автор конфигурации не становится корневым
// инициатором даже с READY ключом. Shared-only targets сохраняют прежний путь; launch и сам вызов
// отдельно повторяют проверки изменяемых grants и credentials.
func (repository *Repository) requireNoninteractiveTargetCredentials(ctx context.Context, tx pgx.Tx, current scope, projectID string, target entity.RunTarget, source, pinnedDigest string) error {
	if source != "SCHEDULE" && source != "INTEGRATION" {
		return errs.ErrInvalid
	}
	refs := []string{target.Ref}
	var workflow *entity.WorkflowVersion
	switch target.Type {
	case "AGENT":
	case "WORKFLOW":
		var name, versionID, versionRef, digest, coordinatorRef, coordinatorName string
		var spec []byte
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": projectID, "workflow_ref": target.Ref,
		}).Scan(&name, &versionID, &versionRef, &spec, &digest, &coordinatorRef, &coordinatorName); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		} else if err != nil {
			return errs.ErrUnavailable
		}
		workflow = &entity.WorkflowVersion{}
		if json.Unmarshal(spec, workflow) != nil || !validWorkflowVersion(*workflow) || workflow.CoordinatorAgentRef != coordinatorRef || pinnedDigest != "" && digest != pinnedDigest {
			return errs.ErrConflict
		}
	default:
		return errs.ErrInvalid
	}
	return repository.requirePersonalLaunchCredentials(ctx, tx, current, projectID,
		personalLaunchTargets(refs, workflow), personalLaunchContext{source: source})
}
