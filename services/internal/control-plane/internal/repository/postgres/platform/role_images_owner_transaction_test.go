package platform

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestRoleImageOwnerCommitRetryBoundary exercises the actual commit classifier and cleanup
// inside the shared retry loop, including unknown acknowledgements and cancelled cleanup context.
// It never executes an external effect or prints the deliberately private synthetic driver text.
// TestRoleImageOwnerCommitRetryBoundary проверяет настоящий commit classifier и cleanup внутри
// shared retry loop, включая неизвестный acknowledgement и отмену caller. Внешних effects нет;
// намеренно приватный synthetic driver text не печатается.
func TestRoleImageOwnerCommitRetryBoundary(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		commit, rollback error
		attempts         int
	}{
		{"serialization", &pgconn.PgError{Code: "40001", Message: "synthetic private driver text"}, pgx.ErrTxClosed, 3},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, pgx.ErrTxClosed, 3},
		{"known-abort", pgx.ErrTxCommitRollback, pgx.ErrTxClosed, 3},
		{"unknown-commit", io.EOF, pgx.ErrTxClosed, 1},
		{"cleanup-failure", &pgconn.PgError{Code: "40001"}, io.EOF, 1},
		{"permanent", &pgconn.PgError{Code: "23505"}, pgx.ErrTxClosed, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result, err := retryRoleImageWorkerTransaction(ctx, func() (_ int, resultErr error) {
				calls++
				tx := &bootstrapCleanupTx{commit: scenario.commit, rollback: scenario.rollback}
				defer func() {
					rollbackAssistantLockedRead(ctx, tx, &resultErr)
					if tx.rollbacks != 1 || tx.canceledCleanup {
						t.Fatal("cleanup did not complete in independent bounded context")
					}
				}()
				if scenario.name == "cleanup-failure" {
					cancel()
				}
				return 42, commitRoleImageOwnerTransaction(ctx, tx)
			})
			wanted := errs.ErrUnavailable
			if scenario.name == "permanent" {
				wanted = errs.ErrConflict
			}
			if calls != scenario.attempts || result != 0 || !errors.Is(err, wanted) {
				t.Fatal("commit or cleanup crossed the safe retry boundary")
			}
		})
	}
}
