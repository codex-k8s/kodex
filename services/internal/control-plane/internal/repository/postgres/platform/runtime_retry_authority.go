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

// queryRuntimeRetryAuthority embeds exact tenant run target/project lookup used to reapply
// ordinary launch permissions before retry receipt/OCC handling. It does not itself authorize the
// target or create a retry attempt.
// queryRuntimeRetryAuthority встраивает поиск цели и проекта точного запуска организации для
// повторной проверки обычных launch permissions до retry receipt/OCC. Сам не проверяет доступ к
// цели и не создаёт новую попытку.
//
//go:embed sql/runtime_retry_authority.sql
var queryRuntimeRetryAuthority string

// authorizeSessionTurnTarget resolves the active session target and delegates a typed LaunchRun
// admission using the original principal/mutation metadata. The current target authority is
// rechecked even for replay before receipt/OCC; missing rows are NotFound and no turn is created.
// authorizeSessionTurnTarget разрешает цель активной сессии и передаёт типизированный LaunchRun на
// проверку с исходными principal/mutation. Текущие права цели перепроверяются даже для replay до
// receipt/OCC; отсутствие строки даёт NotFound, turn не создаётся.
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

// authorizeRetryTarget loads the exact owner run target/project and resolves the same
// permission/target as ordinary LaunchRun before requiring access. Query errors retain
// NotFound/Unavailable mapping; it performs no retry, external replay or transaction commit.
// authorizeRetryTarget загружает точные цель и проект owner run и разрешает те же
// permission/target, что обычный LaunchRun, перед проверкой доступа. Ошибки запроса сохраняют
// NotFound/Unavailable; не выполняет retry, внешний replay и Commit.
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
