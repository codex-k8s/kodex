package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_retry_authority.sql
var queryRuntimeRetryAuthority string

func (repository *Repository) authorizeSessionTurnTarget(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.SessionTurnInput)
	if !ok || payload.SessionRef == "" {
		return errs.ErrInvalid
	}
	var projectID, projectRef, targetType, targetRef string
	if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState, current.organizationID, payload.SessionRef).Scan(&projectID, &projectRef, &targetType, &targetRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		}
		return errs.ErrUnavailable
	}
	// Свежая authority сессии проверяется и для точного replay до receipt/OCC.
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: targetType, Ref: targetRef}}
	return repository.authorizeCommand(ctx, tx, current, nested)
}

func (repository *Repository) authorizeRetryTarget(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.RunCommandInput)
	if !ok {
		return errs.ErrInvalid
	}
	var target entity.RunTarget
	var projectRef string
	if err := tx.QueryRow(ctx, queryRuntimeRetryAuthority, pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_ref": payload.RunRef}).Scan(&target.Type, &target.Ref, &projectRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		}
		return errs.ErrUnavailable
	}
	// Повтор использует тот же целевой permission, что обычный запуск, до чтения receipt и OCC.
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Target: target}
	permission, resolved, err := repository.commandAccessTarget(ctx, tx, current, nested)
	if err != nil {
		return err
	}
	return repository.requireAccess(ctx, tx, current, permission, resolved)
}
