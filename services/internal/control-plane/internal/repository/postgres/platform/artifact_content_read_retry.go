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
	// artifactContentReadAttempts caps owner read attempts at the shared observability maximum so
	// retry accounting uses the same closed bound.
	// artifactContentReadAttempts ограничивает число попыток чтения владельца общим максимумом
	// observability, сохраняя одинаковую границу учёта повторов.
	artifactContentReadAttempts = artifactread.MaximumAttempts
	// artifactContentReadBudget limits when a new retry may start to five seconds; it does not shorten
	// the caller context used by a returned body.
	// artifactContentReadBudget ограничивает начало новых повторов пятью секундами; не сокращает
	// caller context возвращаемого тела ответа.
	artifactContentReadBudget = 5 * time.Second
	// artifactContentReadBackoffBase starts the exponential equal-jitter window at ten milliseconds
	// before the shared maximum is applied.
	// artifactContentReadBackoffBase задаёт начальное окно equal jitter в десять миллисекунд перед
	// экспоненциальным ростом до максимума.
	artifactContentReadBackoffBase = 10 * time.Millisecond
	// artifactContentReadBackoffMaximum caps the jitter window at 250 milliseconds, with the actual
	// sleep also bounded by the remaining retry budget.
	// artifactContentReadBackoffMaximum ограничивает окно jitter значением 250 миллисекунд;
	// фактическое ожидание дополнительно ограничено оставшимся бюджетом.
	artifactContentReadBackoffMaximum = 250 * time.Millisecond
)

// artifactReadSerializationRetry marks a read eligible for a fresh snapshot after observed
// SQLSTATE 40001 and accepted rollback; fallback retains the domain diagnostic.
// artifactReadSerializationRetry помечает чтение для нового snapshot после наблюдаемого SQLSTATE
// 40001 и допустимого rollback; fallback сохраняет доменную диагностику.
type artifactReadSerializationRetry struct{ fallback error }

// Error returns a fixed retry marker without database text, SQL, request fields or private driver
// payloads.
// Error возвращает постоянный текст маркера повтора без текста БД, SQL, полей запроса или
// внутренних данных драйвера.
func (err *artifactReadSerializationRetry) Error() string {
	return "artifact content read serialization retry"
}

// Unwrap exposes the saved domain failure to errors.Is/As while preserving the separate
// serialization-retry marker.
// Unwrap открывает сохранённую доменную ошибку для errors.Is/As, сохраняя отдельный маркер повтора
// после serialization failure.
func (err *artifactReadSerializationRetry) Unwrap() error { return err.fallback }

// retryArtifactContentRead uses the production backoff while keeping the original caller context
// alive for the returned Reader and transport consumer.
// retryArtifactContentRead использует штатный backoff, сохраняя исходный caller context для
// возвращённого Reader и транспортного потребителя.
func retryArtifactContentRead(ctx context.Context, operation func() (platformrepo.ArtifactDownload, error)) (platformrepo.ArtifactDownload, error) {
	return retryArtifactContentReadWithBackoff(ctx, operation, artifactContentReadBackoff)
}

// retryArtifactContentReadWithBackoff retries only typed serialization failures within
// attempt/start-time bounds, waits with caller cancellation and annotates terminal attempt counts;
// other failures stop immediately.
// retryArtifactContentReadWithBackoff повторяет только типизированные serialization failures в
// пределах числа попыток и времени их начала, учитывает отмену caller и terminal attempt count;
// прочие ошибки сразу завершают цикл.
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

// artifactContentReadBackoff returns equal jitter from half to all of a capped exponential window,
// clipped to remaining time; nonpositive remaining time yields zero.
// artifactContentReadBackoff возвращает equal jitter от половины до полного экспоненциального окна
// с верхней границей и ограничением оставшимся временем; неположительный остаток даёт ноль.
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

// artifactContentReadTx wraps one owner read transaction to record its first failure and SQLSTATE
// before domain mapping; body cleanup failure prevents serialization retry.
// artifactContentReadTx оборачивает транзакцию одного чтения владельца для учёта первой ошибки и
// SQLSTATE до доменного преобразования; ошибка очистки тела запрещает повтор.
type artifactContentReadTx struct {
	pgx.Tx
	serializationFailure bool
	bodyCleanupFailed    bool
	stage                string
	diagnostic           artifactread.Diagnostic
	observedFailure      bool
}

// observeFailure stores only the first nonnil failure diagnostic at the current stage, preserving
// the original failure classification.
// observeFailure сохраняет диагностику только первой ненулевой ошибки на текущей стадии, удерживая
// исходную классификацию отказа.
func (tx *artifactContentReadTx) observeFailure(err error) {
	if err != nil && !tx.observedFailure {
		tx.diagnostic = artifactReadDiagnostic(tx.stage, err)
		tx.observedFailure = true
	}
}

// observe records the first failure and latches serializationFailure whenever the error chain
// contains PostgreSQL SQLSTATE 40001.
// observe учитывает первую ошибку и устанавливает serializationFailure, если цепочка ошибки
// содержит PostgreSQL SQLSTATE 40001.
func (tx *artifactContentReadTx) observe(err error) {
	tx.observeFailure(err)
	var cause *pgconn.PgError
	if errors.As(err, &cause) && cause.Code == "40001" {
		tx.serializationFailure = true
	}
}

// QueryRow forwards the original query, context and arguments and wraps the lazy row so a later
// Scan error is observed.
// QueryRow передаёт исходные запрос, context и аргументы, оборачивая отложенную строку для учёта
// последующей ошибки Scan.
func (tx *artifactContentReadTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return artifactContentReadRow{Row: tx.Tx.QueryRow(ctx, sql, args...), tx: tx}
}

// Query observes immediate query failure and wraps successful rows for later Scan/Err observation
// without changing their close lifecycle.
// Query учитывает немедленный отказ запроса и оборачивает успешные rows для учёта последующих
// Scan/Err, не меняя их жизненный цикл закрытия.
func (tx *artifactContentReadTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	tx.observe(err)
	if err != nil {
		return rows, err
	}
	return artifactContentReadRows{Rows: rows, tx: tx}, nil
}

// Exec forwards the exact statement and parameters, observes its failure and returns the original
// command tag and error.
// Exec передаёт точные statement и параметры, учитывает отказ и возвращает исходные command tag и
// ошибку.
func (tx *artifactContentReadTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	tx.observe(err)
	return tag, err
}

// Commit uses the caller context for the underlying commit and observes its error without changing
// the returned outcome.
// Commit использует caller context для commit исходной транзакции и учитывает ошибку без изменения
// возвращаемого исхода.
func (tx *artifactContentReadTx) Commit(ctx context.Context) error {
	err := tx.Tx.Commit(ctx)
	tx.observe(err)
	return err
}

// finish adds the first driver or domain diagnostic, rolls back using an independent one-second
// context and marks eligible failed reads for retry only after rollback succeeds or returns
// ErrTxClosed.
// finish добавляет первую driver либо domain диагностику, выполняет rollback с независимым
// секундным context и разрешает подходящий повтор только при успешном rollback либо ErrTxClosed.
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

// artifactContentReadRow retains the originating read transaction beside a pgx.Row so lazy scan
// errors participate in retry diagnostics.
// artifactContentReadRow хранит исходную транзакцию чтения рядом с pgx.Row, чтобы отложенные
// ошибки Scan учитывались в диагностике повторов.
type artifactContentReadRow struct {
	pgx.Row
	tx *artifactContentReadTx
}

// Scan forwards scan destinations unchanged, records the returned failure in the owning read
// transaction and returns it.
// Scan передаёт destinations без изменения, учитывает отказ в транзакции чтения и возвращает
// исходную ошибку.
func (row artifactContentReadRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	row.tx.observe(err)
	return err
}

// artifactContentReadRows retains the originating transaction beside pgx.Rows while leaving
// iteration and Close with the embedded rows implementation.
// artifactContentReadRows хранит исходную транзакцию рядом с pgx.Rows, оставляя итерацию и Close
// вложенной реализации rows.
type artifactContentReadRows struct {
	pgx.Rows
	tx *artifactContentReadTx
}

// Scan observes each row scan failure through the read transaction without replacing the original
// error or destinations.
// Scan учитывает отказ сканирования строки через транзакцию чтения, сохраняя исходную ошибку и
// destinations.
func (rows artifactContentReadRows) Scan(dest ...any) error {
	err := rows.Rows.Scan(dest...)
	rows.tx.observe(err)
	return err
}

// Err observes deferred iteration failure when the caller reads Err and returns that same error
// without closing rows itself.
// Err учитывает отложенный отказ итерации при чтении Err и возвращает ту же ошибку, самостоятельно
// не закрывая rows.
func (rows artifactContentReadRows) Err() error {
	err := rows.Rows.Err()
	rows.tx.observe(err)
	return err
}
