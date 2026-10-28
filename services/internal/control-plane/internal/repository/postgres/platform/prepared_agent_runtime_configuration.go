package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// prepareAgentRuntimeConfiguration names the closed ordinary-agent plan operation; it never aliases assistant self-configuration.
// prepareAgentRuntimeConfiguration обозначает закрытую операцию плана обычного сотрудника и не подменяет самонастройку помощника.
const prepareAgentRuntimeConfiguration = "PREPARE_AGENT_RUNTIME_CONFIGURATION"

// queryPreparedAgentRuntimeTarget locks only an ordinary agent in the exact active project and tenant, excluding every assistant profile.
// queryPreparedAgentRuntimeTarget блокирует только обычного сотрудника точного активного проекта и организации, исключая все профили помощников.
//
//go:embed sql/assistant_agent_runtime__resolve_target.sql
var queryPreparedAgentRuntimeTarget string

// preparedRuntimeConfigurationOperation recognizes the two specialized commands without merging their target authorization policies.
// preparedRuntimeConfigurationOperation распознаёт две специализированные команды, сохраняя раздельные политики авторизации целей.
func preparedRuntimeConfigurationOperation(kind string) bool {
	return kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" || kind == prepareAgentRuntimeConfiguration
}

// preparedRuntimeTarget resolves ordinary employees by server-bound project, while assistant operations retain their existing protected resolver.
// preparedRuntimeTarget разрешает обычных сотрудников по назначенному сервером проекту, а операции помощников сохраняют прежний защищённый resolver.
func (repository *Repository) preparedRuntimeTarget(ctx context.Context, tx pgx.Tx, current scope, agentRef, projectRef string, ordinary bool) (assistantConfigurationTarget, error) {
	if !ordinary {
		return repository.assistantConfigurationTarget(ctx, tx, current, agentRef)
	}
	var target assistantConfigurationTarget
	if projectRef == "" {
		return target, errs.ErrNotFound
	}
	err := tx.QueryRow(ctx, queryPreparedAgentRuntimeTarget, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": agentRef,
		"project_ref": projectRef, "authority_project": current.authorityProjectID,
	}).Scan(&target.scopeKind, &target.projectRef, &target.profileRef, &target.name, &target.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, errs.ErrNotFound
	}
	if err != nil {
		return target, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	permission, access, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.manage", agentRef)
	if err != nil {
		return target, err
	}
	if err := repository.requireAccess(ctx, tx, current, permission, access); err != nil {
		return target, err
	}
	target.ordinary = true
	return target, nil
}
