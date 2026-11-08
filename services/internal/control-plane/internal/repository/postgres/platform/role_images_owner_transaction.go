package platform

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// commitRoleImageOwnerTransaction marks only known serialization/deadlock aborts for the owner
// retry loop, preserving typed write mappings. An unknown network/commit outcome is unavailable
// without a retry marker; the caller must reconcile its original idempotency receipt explicitly.
// commitRoleImageOwnerTransaction помечает для owner retry loop только известные serialization/
// deadlock aborts, сохраняя typed write mappings. Неизвестный network/commit outcome даёт
// unavailable без retry marker; caller явно сверяет исходную idempotency receipt.
func commitRoleImageOwnerTransaction(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return serializableTransactionError(err, mapRoleImageWriteError(err))
	}
	return nil
}
