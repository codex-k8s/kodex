package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// runtimeEnvironmentLockTuple stores the six fields returned by the locked environment query; it
// does not itself establish owner access or permission to mutate the environment.
// runtimeEnvironmentLockTuple хранит шесть полей блокированного окружения; сам по себе не
// подтверждает доступ владельца или право изменения окружения.
type runtimeEnvironmentLockTuple struct {
	environmentID, projectID, projectRef, currentVersionID string
	environmentVersion, currentRevision                    int64
}

// lockRuntimeEnvironmentTuple reads the exact tenant/ref lock tuple and maps missing/driver errors;
// caller retains transaction, owner authorization, OCC and subsequent lifecycle decisions.
// lockRuntimeEnvironmentTuple читает точный tenant/ref lock tuple и преобразует ошибки отсутствия
// и драйвера; транзакция, проверка владельца, OCC и lifecycle остаются у вызывающего.
func lockRuntimeEnvironmentTuple(ctx context.Context, tx pgx.Tx, organizationID, ref string) (runtimeEnvironmentLockTuple, error) {
	var locked runtimeEnvironmentLockTuple
	err := tx.QueryRow(ctx, queryRuntimeConfigurationLockEnvironment, organizationID, ref).Scan(
		&locked.environmentID, &locked.projectID, &locked.projectRef, &locked.environmentVersion, &locked.currentVersionID, &locked.currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return locked, errs.ErrNotFound
	}
	if err != nil {
		return locked, errs.ErrUnavailable
	}
	return locked, nil
}
