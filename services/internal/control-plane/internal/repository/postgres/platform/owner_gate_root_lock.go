package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// lockOwnerGateRootTx reuses the existing runtime root mutex after the caller resolved exact tenant lineage.
// It grants no authority and changes no state. Gate decisions take this lock before reading the sibling
// predicate; a later statement must use the post-lock snapshot at READ COMMITTED.
// lockOwnerGateRootTx использует existing runtime root mutex после разрешения exact tenant lineage.
// Не выдаёт authority и не меняет state. Gate decisions блокируют root до чтения sibling predicate;
// последующий statement использует post-lock snapshot при READ COMMITTED.
func lockOwnerGateRootTx(ctx context.Context, tx pgx.Tx, rootID string) (string, error) {
	var id, state string
	err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionLockRootRun, rootID).Scan(&id, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrNotFound
	}
	if err != nil {
		return "", serializableTransactionError(err, errs.ErrUnavailable)
	}
	if id != rootID || state == "" {
		return "", errs.ErrUnavailable
	}
	return state, nil
}
