package platform

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// bootstrapCleanupTx records actual cleanup contexts and returns selected private SQL/commit errors.
// bootstrapCleanupTx наблюдает настоящие cleanup contexts и возвращает выбранные SQL/commit ошибки.
type bootstrapCleanupTx struct {
	pgx.Tx
	cause, commit, rollback error
	rollbacks               int
	canceledCleanup         bool
}

// Exec exposes a driver failure to the real Bootstrap transaction observer.
// Exec передаёт driver failure настоящему Bootstrap transaction observer.
func (tx *bootstrapCleanupTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.cause
}

// Commit returns one controlled outcome, including unknown transport acknowledgement.
// Commit возвращает управляемый outcome, включая неизвестное transport acknowledgement.
func (tx *bootstrapCleanupTx) Commit(context.Context) error { return tx.commit }

// Rollback records whether independent bounded cleanup was attempted despite caller cancellation.
// Rollback отмечает независимый bounded cleanup несмотря на отмену вызывающего.
func (tx *bootstrapCleanupTx) Rollback(ctx context.Context) error {
	tx.rollbacks++
	tx.canceledCleanup = ctx.Err() != nil
	return tx.rollback
}

// TestBootstrapAttemptRequiresConfirmedCleanup verifies that retry permission comes from an exact
// driver abort plus cleanup; unknown commit or failed rollback cannot inherit an earlier40001.
// TestBootstrapAttemptRequiresConfirmedCleanup проверяет допуск повтора лишь по exact driver abort
// и cleanup; неизвестный commit либо отказ rollback не наследуют прежний40001.
func TestBootstrapAttemptRequiresConfirmedCleanup(t *testing.T) {
	driver := &pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL_PASSWORD_SENTINEL"}
	for _, test := range []struct {
		name                         string
		rollback, commit, error      error
		commitStarted, cancel, retry bool
	}{
		{name: "statement-abort", error: driver, retry: true},
		{name: "deadlock-abort", error: &pgconn.PgError{Code: "40P01"}, retry: true},
		{name: "permission", error: &pgconn.PgError{Code: "42501"}},
		{name: "rollback-failed", error: driver, rollback: io.ErrUnexpectedEOF},
		{name: "unproven-closed", error: driver, rollback: pgx.ErrTxClosed},
		{name: "known-commit-abort", error: driver, commit: driver, commitStarted: true, rollback: pgx.ErrTxClosed, retry: true},
		{name: "unknown-commit", error: driver, commit: io.EOF, commitStarted: true, rollback: pgx.ErrTxClosed},
		{name: "canceled", error: driver, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			raw := &bootstrapCleanupTx{cause: test.error, commit: test.commit, rollback: test.rollback}
			tx := &bootstrapTx{Tx: raw}
			_, _ = tx.Exec(ctx, "fixture statement")
			failure := error(errs.ErrUnavailable)
			if test.commitStarted {
				failure = tx.Commit(ctx)
			}
			if test.cancel {
				cancel()
			}
			finishBootstrapAttempt(ctx, tx, &failure)
			var outcome *bootstrapAttemptFailure
			if !errors.As(failure, &outcome) || outcome.retry != test.retry || raw.rollbacks != 1 || raw.canceledCleanup || !errors.Is(failure, test.error) {
				t.Fatal("bootstrap cleanup/retry fence changed")
			}
			if strings.Contains(failure.Error(), "PRIVATE_") {
				t.Fatal("bootstrap attempt disclosed private payload")
			}
		})
	}
}

// TestBootstrapRetryPolicyPreservesFinalCause checks the inherited attempt limit, permanent
// failures and exhausted caller budget without repeating an unknown or unclean transaction.
// TestBootstrapRetryPolicyPreservesFinalCause проверяет прежний предел попыток, permanent failures
// и caller budget без повтора неизвестной либо неочищенной транзакции.
func TestBootstrapRetryPolicyPreservesFinalCause(t *testing.T) {
	for _, test := range []struct {
		name    string
		cause   error
		success bool
		want    int
	}{
		{"serialization-success", &pgconn.PgError{Code: "40001"}, true, 2},
		{"deadlock-exhausted", &pgconn.PgError{Code: "40P01"}, false, 3},
		{"permission-permanent", &pgconn.PgError{Code: "42501"}, false, 1},
		{"driver-permanent", io.EOF, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			err := retryBootstrap(t.Context(), func(ctx context.Context) error {
				attempts++
				if test.success && attempts == 2 {
					return nil
				}
				raw := &bootstrapCleanupTx{cause: test.cause}
				tx := &bootstrapTx{Tx: raw}
				_, _ = tx.Exec(ctx, "fixture statement")
				failure := bootstrapFailure("fixture phase", test.cause)
				finishBootstrapAttempt(ctx, tx, &failure)
				return failure
			})
			if attempts != test.want || test.success != (err == nil) || err != nil && !errors.Is(err, test.cause) {
				t.Fatal("bootstrap retry lost cause or changed attempt bounds")
			}
		})
	}
	t.Run("canceled-before-attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		attempts := 0
		err := retryBootstrap(ctx, func(context.Context) error { attempts++; return nil })
		if attempts != 0 || !errors.Is(err, context.Canceled) {
			t.Fatal("canceled bootstrap attempted work")
		}
	})
	t.Run("cancel-before-next-attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		attempts := 0
		driver := &pgconn.PgError{Code: "40001"}
		err := retryBootstrap(ctx, func(ctx context.Context) error {
			attempts++
			raw := &bootstrapCleanupTx{cause: driver}
			tx := &bootstrapTx{Tx: raw}
			_, _ = tx.Exec(ctx, "fixture statement")
			failure := error(driver)
			finishBootstrapAttempt(ctx, tx, &failure)
			cancel()
			return failure
		})
		if attempts != 1 || !errors.Is(err, context.Canceled) || !errors.Is(err, driver) {
			t.Fatal("bootstrap retried after caller cancellation")
		}
	})
	t.Run("exhausted-budget-before-attempt", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
		defer cancel()
		attempts := 0
		err := retryBootstrap(ctx, func(context.Context) error { attempts++; return nil })
		if attempts != 0 || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("bootstrap extended exhausted caller budget")
		}
	})
	t.Run("nested-marker-cannot-bypass-cleanup", func(t *testing.T) {
		attempts := 0
		driver := &pgconn.PgError{Code: "40001"}
		err := retryBootstrap(t.Context(), func(ctx context.Context) error {
			attempts++
			raw := &bootstrapCleanupTx{cause: driver, rollback: io.EOF}
			tx := &bootstrapTx{Tx: raw}
			_, _ = tx.Exec(ctx, "fixture statement")
			failure := serializableTransactionError(driver, errs.ErrUnavailable)
			finishBootstrapAttempt(ctx, tx, &failure)
			return failure
		})
		if attempts != 1 || !errors.Is(err, driver) || !errors.Is(err, io.EOF) {
			t.Fatal("nested helper bypassed bootstrap cleanup fence")
		}
	})
}

// bootstrapObservedRows supplies a real iterator/scan error to the startup-only observer.
// bootstrapObservedRows передаёт настоящий iterator/scan error startup-only observer.
type bootstrapObservedRows struct {
	pgx.Rows
	cause  error
	closed bool
}

// Next ends the controlled iterator; Err holds the original private driver failure.
// Next завершает управляемый iterator; Err хранит исходный приватный driver failure.
func (rows *bootstrapObservedRows) Next() bool { return false }

// Err retains the synthetic private driver cause without formatting it.
// Err сохраняет синтетическую приватную driver cause без форматирования.
func (rows *bootstrapObservedRows) Err() error { return rows.cause }

// Scan returns a row decoder failure to test capture before domain error mapping.
// Scan возвращает row decoder failure для проверки capture до domain mapping.
func (rows *bootstrapObservedRows) Scan(...any) error { return rows.cause }

// Close records delegated cleanup independently of the observed failure.
// Close отмечает делегированный cleanup независимо от наблюдаемого отказа.
func (rows *bootstrapObservedRows) Close() { rows.closed = true }

// TestBootstrapObserverPreservesDeferredRowFailure covers QueryRow/iterator/Scan/Close paths
// and keeps ordinary NoRows absence out of retry eligibility.
// TestBootstrapObserverPreservesDeferredRowFailure проверяет QueryRow/iterator/Scan/Close
// и не превращает обычное NoRows отсутствие в retry eligibility.
func TestBootstrapObserverPreservesDeferredRowFailure(t *testing.T) {
	driver := &pgconn.PgError{Code: "40001", Message: "PRIVATE_ROW_SENTINEL"}
	for _, mode := range []string{"row", "iterator", "scan", "close", "not-found"} {
		t.Run(mode, func(t *testing.T) {
			tx := &bootstrapTx{}
			row := bootstrapRow{Row: bootstrapFaultRow{cause: driver}, tx: tx}
			source := &bootstrapObservedRows{cause: driver}
			rows := bootstrapRows{Rows: source, tx: tx}
			switch mode {
			case "row":
				_ = row.Scan()
			case "iterator":
				_ = rows.Next()
			case "scan":
				_ = rows.Scan()
			case "close":
				rows.Close()
				if !source.closed {
					t.Fatal("driver rows not closed")
				}
			case "not-found":
				_ = (bootstrapRow{Row: bootstrapFaultRow{cause: pgx.ErrNoRows}, tx: tx}).Scan()
				if tx.cause != nil {
					t.Fatal("ordinary absence became a retry cause")
				}
				return
			}
			if !errors.Is(tx.cause, driver) {
				t.Fatal("deferred SQL error disappeared behind domain mapping")
			}
		})
	}
}
