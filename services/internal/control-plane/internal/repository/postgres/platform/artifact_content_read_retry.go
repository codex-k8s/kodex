package platform

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	artifactContentReadAttempts       = artifactread.MaximumAttempts
	artifactContentReadBudget         = 5 * time.Second
	artifactContentReadBackoffBase    = 10 * time.Millisecond
	artifactContentReadBackoffMaximum = 250 * time.Millisecond
)

type artifactReadSerializationRetry struct{ fallback error }

func (err *artifactReadSerializationRetry) Error() string {
	return "artifact content read serialization retry"
}
func (err *artifactReadSerializationRetry) Unwrap() error { return err.fallback }

// Reader использует исходный caller context. Его нельзя отменять при возврате
// из retry helper: тело ещё предстоит передать транспортному consumer.
func retryArtifactContentRead(ctx context.Context, operation func() (platformrepo.ArtifactDownload, error)) (platformrepo.ArtifactDownload, error) {
	return retryArtifactContentReadWithBackoff(ctx, operation, artifactContentReadBackoff)
}

// Backoff подменяется только в адресных unit-тестах. Production policy закрыта:
// максимум восемь попыток и прежний бюджет начала повторов, без нового context
// для выдаваемого Reader и без изменения общего mutation retry.
func retryArtifactContentReadWithBackoff(ctx context.Context, operation func() (platformrepo.ArtifactDownload, error), backoff func(int, time.Duration) time.Duration) (platformrepo.ArtifactDownload, error) {
	deadline := time.Now().Add(artifactContentReadBudget)
	var fallback error = errs.ErrUnavailable
	for attempt := 0; attempt < artifactContentReadAttempts; attempt++ {
		if ctx.Err() != nil || (attempt > 0 && !time.Now().Before(deadline)) {
			if _, ok := artifactread.From(fallback); !ok {
				fallback = artifactread.WithFailure(fallback, artifactReadDiagnostic(artifactread.StageRetry, ctx.Err()))
			}
			return platformrepo.ArtifactDownload{}, artifactread.WithAttempt(fallback, attempt, true)
		}
		result, err := operation()
		if err == nil {
			return result, nil
		}
		var retry *artifactReadSerializationRetry
		if !errors.As(err, &retry) {
			return platformrepo.ArtifactDownload{}, artifactread.WithAttempt(err, attempt+1, false)
		}
		fallback = retry.fallback
		if attempt+1 == artifactContentReadAttempts || ctx.Err() != nil || !time.Now().Before(deadline) {
			return platformrepo.ArtifactDownload{}, artifactread.WithAttempt(retry.fallback, attempt+1, true)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return platformrepo.ArtifactDownload{}, artifactread.WithAttempt(retry.fallback, attempt+1, true)
		}
		delay := backoff(attempt+1, remaining)
		if delay > remaining {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return platformrepo.ArtifactDownload{}, artifactread.WithAttempt(retry.fallback, attempt+1, true)
		case <-timer.C:
		}
	}
	return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
}

// Equal jitter разносит одновременные новые SERIALIZABLE snapshots. Он меняет
// только ожидание; authority, rollback и граница выдачи результата прежние.
func artifactContentReadBackoff(attempt int, remaining time.Duration) time.Duration {
	if remaining <= 0 {
		return 0
	}
	window := artifactContentReadBackoffBase
	for step := 1; step < attempt && window < artifactContentReadBackoffMaximum; step++ {
		window *= 2
	}
	if window > artifactContentReadBackoffMaximum {
		window = artifactContentReadBackoffMaximum
	}
	minimum := window / 2
	delay := minimum + time.Duration(rand.Int64N(int64(window-minimum)+1))
	if delay > remaining {
		return remaining
	}
	return delay
}

// Обёртка принадлежит одному owner READ и замечает SQLSTATE до domain helpers,
// которые намеренно скрывают driver errors. Payload и SQL не сохраняются.
type artifactContentReadTx struct {
	pgx.Tx
	serializationFailure bool
	bodyCleanupFailed    bool
	stage                string
	diagnostic           artifactread.Diagnostic
	observedFailure      bool
}

func (tx *artifactContentReadTx) observeFailure(err error) {
	if err != nil && !tx.observedFailure {
		tx.diagnostic = artifactReadDiagnostic(tx.stage, err)
		tx.observedFailure = true
	}
}

func (tx *artifactContentReadTx) observe(err error) {
	tx.observeFailure(err)
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
	if *resultError != nil {
		diagnostic := tx.diagnostic
		if !tx.observedFailure {
			diagnostic = artifactread.Diagnostic{Stage: tx.stage, Class: artifactread.ClassDomain}
		}
		*resultError = artifactread.WithFailure(*resultError, diagnostic)
	}
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
