package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryAssistantConfigurationResolveTarget embeds locked assistant identity resolution,
// distinguishing the organization system assistant from an active project assistant profile and
// respecting authority_project. An ordinary agent does not become an assistant through an empty
// request project.
// queryAssistantConfigurationResolveTarget встраивает разрешение assistant identity с блокировкой,
// отличая organization system assistant от active project assistant profile и учитывая
// authority_project. Обычный agent не становится assistant из-за пустого проекта запроса.
//
//go:embed sql/assistant_configuration__resolve_target.sql
var queryAssistantConfigurationResolveTarget string

// assistantConfigurationTarget carries the server-resolved assistant scope, project/profile
// locators, display name and agent version for subsequent authorization and immutable plan pins.
// assistantConfigurationTarget несёт разрешённые сервером assistant scope, project/profile
// locators, отображаемое имя и agent version для последующей авторизации и неизменяемых plan pins.
type assistantConfigurationTarget struct {
	scopeKind, projectRef, profileRef, name string
	version                                 int64
}

// assistantConfigurationTarget resolves an assistant through the mutation-oriented target path;
// payload locators do not supply scope, profile or authority.
// assistantConfigurationTarget разрешает assistant через target path для изменения; payload
// locators не задают scope, profile или authority.
func (repository *Repository) assistantConfigurationTarget(ctx context.Context, tx pgx.Tx, current scope, agentRef string) (assistantConfigurationTarget, error) {
	return repository.assistantConfigurationTargetForRead(ctx, tx, current, agentRef, false)
}

// assistantConfigurationTargetForRead locks and resolves canonical assistant identity, hides
// unsupported scope as NotFound and applies either ordinary runtime view eligibility or
// organization/project mutation authority. SYSTEM retains organization permission rather than
// borrowing project agent.view; locked-read driver errors retain retry classification.
// assistantConfigurationTargetForRead блокирует и разрешает каноническую assistant identity,
// скрывает неподдерживаемый scope как NotFound и применяет обычную runtime view eligibility либо
// organization/project mutation authority. SYSTEM сохраняет organization permission, не заимствуя
// project agent.view; locked-read driver errors сохраняют retry classification.
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
