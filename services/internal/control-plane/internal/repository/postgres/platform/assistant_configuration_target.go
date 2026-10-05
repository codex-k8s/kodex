package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_configuration__resolve_target.sql
var queryAssistantConfigurationResolveTarget string

type assistantConfigurationTarget struct {
	scopeKind, projectRef, profileRef, name string
	version                                 int64
}

// Обычный Agent не превращается в помощника из-за пустого projectRef запроса.
func (repository *Repository) assistantConfigurationTarget(ctx context.Context, tx pgx.Tx, current scope, agentRef string) (assistantConfigurationTarget, error) {
	return repository.assistantConfigurationTargetForRead(ctx, tx, current, agentRef, false)
}

func (repository *Repository) assistantConfigurationTargetForRead(ctx context.Context, tx pgx.Tx, current scope, agentRef string, readOnly bool) (assistantConfigurationTarget, error) {
	var target assistantConfigurationTarget
	err := tx.QueryRow(ctx, queryAssistantConfigurationResolveTarget, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": agentRef, "authority_project": current.authorityProjectID,
	}).Scan(&target.scopeKind, &target.projectRef, &target.profileRef, &target.name, &target.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, errs.ErrNotFound
	}
	if err != nil {
		return target, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if target.scopeKind != "ORGANIZATION" && target.scopeKind != "PROJECT" {
		return target, errs.ErrNotFound
	}
	if readOnly {
		// Тот же predicate, что у GetAgentRuntimeConfiguration; SYSTEM не получает
		// project agent.view вместо канонического organization.manage.
		permission, access, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agentRef)
		if err != nil {
			return target, err
		}
		if err := repository.requireAccess(ctx, tx, current, permission, access); err != nil {
			return target, err
		}
	} else if target.scopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return target, err
		}
	} else if target.scopeKind == "PROJECT" {
		permission, access, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.manage", agentRef)
		if err != nil {
			return target, err
		}
		if err := repository.requireAccess(ctx, tx, current, permission, access); err != nil {
			return target, err
		}
	} else {
		return target, errs.ErrNotFound
	}
	return target, nil
}
