package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type artifactReadSerializationRetry struct{ fallback error }

func (err *artifactReadSerializationRetry) Error() string {
	return "artifact content read serialization retry"
}
func (err *artifactReadSerializationRetry) Unwrap() error { return err.fallback }

// Reader использует исходный caller context. Его нельзя отменять при возврате
// из retry helper: тело ещё предстоит передать транспортному consumer.
func retryArtifactContentRead(ctx context.Context, operation func() (platformrepo.ArtifactDownload, error)) (platformrepo.ArtifactDownload, error) {
	deadline := time.Now().Add(5 * time.Second)
	var fallback error = errs.ErrUnavailable
	for attempt := 0; attempt < serializableTransactionAttempts; attempt++ {
		if ctx.Err() != nil || (attempt > 0 && !time.Now().Before(deadline)) {
			return platformrepo.ArtifactDownload{}, fallback
		}
		result, err := operation()
		if err == nil {
			return result, nil
		}
		var retry *artifactReadSerializationRetry
		if !errors.As(err, &retry) {
			return platformrepo.ArtifactDownload{}, err
		}
		fallback = retry.fallback
		if attempt+1 == serializableTransactionAttempts || ctx.Err() != nil || !time.Now().Before(deadline) {
			return platformrepo.ArtifactDownload{}, retry.fallback
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return platformrepo.ArtifactDownload{}, retry.fallback
		case <-timer.C:
		}
	}
	return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
}

// Обёртка принадлежит одному owner READ и замечает SQLSTATE до domain helpers,
// которые намеренно скрывают driver errors. Payload и SQL не сохраняются.
type artifactContentReadTx struct {
	pgx.Tx
	serializationFailure bool
	bodyCleanupFailed    bool
}

func (tx *artifactContentReadTx) observe(err error) {
	var cause *pgconn.PgError
	if errors.As(err, &cause) && cause.Code == "40001" {
		tx.serializationFailure = true
	}
}

func (tx *artifactContentReadTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return artifactContentReadRow{Row: tx.Tx.QueryRow(ctx, sql, args...), tx: tx}
}

func (tx *artifactContentReadTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	tx.observe(err)
	if err != nil {
		return rows, err
	}
	return artifactContentReadRows{Rows: rows, tx: tx}, nil
}

func (tx *artifactContentReadTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	tx.observe(err)
	return tag, err
}

func (tx *artifactContentReadTx) Commit(ctx context.Context) error {
	err := tx.Tx.Commit(ctx)
	tx.observe(err)
	return err
}

func (tx *artifactContentReadTx) finish(ctx context.Context, resultError *error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := tx.Tx.Rollback(cleanup)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return
	}
	if tx.serializationFailure && !tx.bodyCleanupFailed && *resultError != nil &&
		(errors.Is(*resultError, errs.ErrUnavailable) || errors.Is(*resultError, errs.ErrConflict)) {
		*resultError = &artifactReadSerializationRetry{fallback: *resultError}
	}
}

type artifactContentReadRow struct {
	pgx.Row
	tx *artifactContentReadTx
}

func (row artifactContentReadRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	row.tx.observe(err)
	return err
}

type artifactContentReadRows struct {
	pgx.Rows
	tx *artifactContentReadTx
}

func (rows artifactContentReadRows) Scan(dest ...any) error {
	err := rows.Rows.Scan(dest...)
	rows.tx.observe(err)
	return err
}
func (rows artifactContentReadRows) Err() error {
	err := rows.Rows.Err()
	rows.tx.observe(err)
	return err
}
