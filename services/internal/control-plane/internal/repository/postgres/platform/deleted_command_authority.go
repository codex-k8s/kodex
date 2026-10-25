package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryCommandDeletedAccessTarget embeds tombstone owner/project/related-ref resolution for
// deleted integrations, schedules and runtime environments only. These rows support exact
// specialized delete replay, not ordinary catalog visibility.
// queryCommandDeletedAccessTarget встраивает разрешение owner/project/related refs tombstone
// только удалённых integrations, schedules и runtime environments. Строки обслуживают точный
// replay специализированного delete, а не обычную видимость каталога.
//
//go:embed sql/command_deleted_access_target.sql
var queryCommandDeletedAccessTarget string

// authorizeDeletedCommand maps typed replay targets to closed permissions, resolves tombstone
// ownership and rejects project-boundary mismatches as NotFound. It rechecks command and read
// visibility, using project visibility for runtime environments, then returns the tombstone
// version. No tombstone returns0,nil; this method neither revives the resource nor authorizes a
// new generic delete.
// authorizeDeletedCommand сопоставляет typed replay targets с закрытыми permissions, разрешает
// владельца tombstone и отклоняет нарушение project boundary как NotFound. Перепроверяет command и
// read visibility, используя видимость проекта для runtime environments, затем возвращает версию
// tombstone. Без tombstone возвращает0,nil; не восстанавливает ресурс и не разрешает новый
// универсальный delete.
func (repository *Repository) authorizeDeletedCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (int64, error) {
	var kind, ref, projectRef, permission string
	switch payload := input.Payload.(type) {
	case command.ConnectionInput:
		kind, ref, permission = "INTEGRATION", payload.Ref, "integration.manage"
	case command.ScheduleInput:
		kind, ref, projectRef, permission = "SCHEDULE", payload.Ref, payload.ProjectRef, "schedule.manage"
	case command.RuntimeEnvironmentLifecycleInput:
		kind, ref, permission = "RUNTIME_ENVIRONMENT", payload.EnvironmentRef, "runtime.environment.delete"
	default:
		return 0, errs.ErrInvalid
	}
	var target resolvedAccessTarget
	var related []byte
	var version int64
	err := tx.QueryRow(ctx, queryCommandDeletedAccessTarget, pgx.StrictNamedArgs{"kind": kind, "ref": ref, "organization_id": current.organizationID}).Scan(
		&target.resourceID, &target.projectID, &target.scope.ProjectRef, &target.ownerSubjectRef, &related, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil || json.Unmarshal(related, &target.scope.RelatedResourceRefs) != nil {
		return 0, errs.ErrUnavailable
	}
	if projectRef != "" && projectRef != target.scope.ProjectRef || current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return 0, errs.ErrNotFound
	}
	target.scope.Kind, target.scope.ResourceKind, target.scope.ResourceRef = "RESOURCE_INSTANCE", kind, ref
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return 0, err
	}
	var readTarget any = target
	if kind == "RUNTIME_ENVIRONMENT" {
		resolved, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: target.scope.ProjectRef})
		if err != nil {
			return 0, err
		}
		readTarget = resolved
	}
	if err := repository.requireAccess(ctx, tx, current, visibilityPermission(kind), readTarget); err != nil {
		return 0, err
	}
	return version, nil
}
