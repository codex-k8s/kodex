package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// artifactReadCleanupStub embeds an unimplemented transaction and implements only rollback
// counting with a chosen error. Unexpected use of other transaction methods is unsupported; the
// stub proves cleanup control flow, not PostgreSQL rollback.
// artifactReadCleanupStub встраивает нереализованную transaction и поддерживает только подсчёт
// rollback с выбранной ошибкой. Прочие transaction methods не поддерживаются; stub проверяет
// cleanup control flow, а не PostgreSQL rollback.
type artifactReadCleanupStub struct {
	pgx.Tx
	rollback error
	calls    int
}

// Rollback increments the cleanup call counter and returns the configured error without inspecting
// context or contacting a backend. It models each attempted rollback, including unsuccessful
// cleanup.
// Rollback увеличивает cleanup call counter и возвращает configured error без проверки context
// либо обращения к backend. Моделирует каждую попытку rollback, включая неуспешный cleanup.
func (tx *artifactReadCleanupStub) Rollback(ctx context.Context) error {
	tx.calls++
	return tx.rollback
}

// TestArtifactContentReadRetryKeepsExhaustedFallbackAndCleanup exhausts serialization-marked read
// retries with immediate backoff and requires exactly one rollback per attempt plus the original
// Unavailable/Conflict fallback. Private SQL sentinel text must not escape; no database or reader
// is opened.
// TestArtifactContentReadRetryKeepsExhaustedFallbackAndCleanup исчерпывает serialization-marked
// read retries с immediate backoff и требует одного rollback на attempt с исходным
// Unavailable/Conflict fallback. Private SQL sentinel text не должен выходить наружу; БД и reader
// не открываются.
func TestArtifactContentReadRetryKeepsExhaustedFallbackAndCleanup(t *testing.T) {
	for _, fallback := range []error{errs.ErrUnavailable, errs.ErrConflict} {
		attempts, rollbacks := 0, 0
		_, err := retryArtifactContentReadWithBackoff(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
			attempts++
			base := &artifactReadCleanupStub{}
			tx := &artifactContentReadTx{Tx: base}
			defer func() { tx.finish(t.Context(), &resultError); rollbacks += base.calls }()
			tx.observe(&pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL_SENTINEL"})
			return platformrepo.ArtifactDownload{}, fallback
		}, artifactReadImmediateBackoff)
		if attempts != artifactContentReadAttempts || rollbacks != artifactContentReadAttempts || !errors.Is(err, fallback) || strings.Contains(err.Error(), "PRIVATE_SQL_SENTINEL") {
			t.Fatal("retry bounds, cleanup or original fallback changed")
		}
	}
}

// artifactReadImmediateBackoff returns zero delay regardless of attempt or remaining budget to
// make exhausted-retry tests deterministic and immediate. Production jitter and waiting are
// exercised separately.
// artifactReadImmediateBackoff возвращает нулевую задержку независимо от attempt и remaining
// budget для детерминированных immediate exhausted-retry tests. Production jitter и ожидание
// проверяются отдельно.
func artifactReadImmediateBackoff(int, time.Duration) time.Duration { return 0 }

// TestArtifactContentReadRetryRecoversFourthAndEighthSlot simulates serialization failures until
// the fourth or last read slot, checking discarded partial grants and rollback count before one
// successful result. It also pins mutation retry count and the five-second read budget; no real
// contention is generated.
// TestArtifactContentReadRetryRecoversFourthAndEighthSlot моделирует serialization failures до
// четвёртого либо последнего read slot, проверяя отброшенные partial grants и rollback count до
// одного success. Также фиксирует mutation retry count и пятисекундный read budget; реальная
// конкуренция не создаётся.
func TestArtifactContentReadRetryRecoversFourthAndEighthSlot(t *testing.T) {
	for _, successAttempt := range []int{4, artifactContentReadAttempts} {
		attempts, rollbacks := 0, 0
		result, err := retryArtifactContentRead(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
			attempts++
			if attempts == successAttempt {
				return platformrepo.ArtifactDownload{GrantRef: "committed-once"}, nil
			}
			base := &artifactReadCleanupStub{}
			tx := &artifactContentReadTx{Tx: base, stage: artifactread.StageAudit}
			defer func() { tx.finish(t.Context(), &resultError); rollbacks += base.calls }()
			tx.observe(&pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL_SENTINEL"})
			return platformrepo.ArtifactDownload{GrantRef: "discarded"}, errs.ErrUnavailable
		})
		if err != nil || attempts != successAttempt || rollbacks != successAttempt-1 || result.GrantRef != "committed-once" {
			t.Fatal("bounded artifact READ did not recover available serialization slot")
		}
	}
	if serializableTransactionAttempts != 3 || artifactContentReadBudget != 5*time.Second {
		t.Fatal("artifact contention recovery changed mutation retry or request budget")
	}
}

// TestArtifactContentReadBackoffJitterAndBudgetBounds samples each exponential jitter window,
// requires variation, clips delay to remaining nanosecond budget and forbids waiting after
// exhaustion. The random-sampling assertion checks the backoff function rather than real
// concurrent transactions.
// TestArtifactContentReadBackoffJitterAndBudgetBounds сэмплирует каждое exponential jitter window,
// требует variation, ограничивает delay оставшимся nanosecond budget и запрещает ожидание после
// exhaustion. Random-sampling assertion проверяет backoff function, а не реальные concurrent
// transactions.
func TestArtifactContentReadBackoffJitterAndBudgetBounds(t *testing.T) {
	for attempt := 1; attempt <= artifactContentReadAttempts+1; attempt++ {
		window := artifactContentReadBackoffBase
		for step := 1; step < attempt && window < artifactContentReadBackoffMaximum; step++ {
			window *= 2
		}
		if window > artifactContentReadBackoffMaximum {
			window = artifactContentReadBackoffMaximum
		}
		seen := map[time.Duration]bool{}
		for sample := 0; sample < 32; sample++ {
			delay := artifactContentReadBackoff(attempt, artifactContentReadBudget)
			if delay < window/2 || delay > window {
				t.Fatal("artifact backoff escaped bounded jitter window")
			}
			seen[delay] = true
			if clipped := artifactContentReadBackoff(attempt, time.Nanosecond); clipped != time.Nanosecond {
				t.Fatal("backoff exceeded remaining budget")
			}
		}
		if len(seen) < 2 {
			t.Fatal("artifact backoff does not separate concurrent retries")
		}
	}
	if artifactContentReadBackoff(1, 0) != 0 || artifactContentReadBackoff(1, -time.Second) != 0 {
		t.Fatal("exhausted budget produced another wait")
	}
}

// TestArtifactContentReadRetryCancellationKeepsPrimaryDiagnostic cancels during backoff after one
// serialization failure and requires the original audit-stage SQLSTATE/attempt diagnostic and
// exhausted flag to survive without another attempt. Private SQL detail remains behind the safe
// diagnostic boundary.
// TestArtifactContentReadRetryCancellationKeepsPrimaryDiagnostic отменяет context во время backoff
// после одного serialization failure и требует сохранения исходных audit-stage SQLSTATE/attempt
// diagnostic и exhausted flag без новой attempt. Private SQL detail остаётся за safe diagnostic
// boundary.
func TestArtifactContentReadRetryCancellationKeepsPrimaryDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	_, err := retryArtifactContentReadWithBackoff(ctx, func() (_ platformrepo.ArtifactDownload, resultError error) {
		attempts++
		tx := &artifactContentReadTx{Tx: &artifactReadCleanupStub{}, stage: artifactread.StageAudit}
		defer tx.finish(ctx, &resultError)
		tx.observe(&pgconn.PgError{Code: "40001", Message: "PRIVATE_SQL"})
		return platformrepo.ArtifactDownload{}, errs.ErrUnavailable
	}, func(int, time.Duration) time.Duration { cancel(); return time.Second })
	diagnostic, ok := artifactread.From(err)
	if !ok || attempts != 1 || !errors.Is(err, errs.ErrUnavailable) || diagnostic.Stage != artifactread.StageAudit || diagnostic.SQLState != "40001" || diagnostic.Attempt != 1 || !diagnostic.RetryExhausted {
		t.Fatal("cancellation hid primary serialization diagnostic or started another attempt")
	}
}

// TestArtifactContentReadRetryRejectsUnknownCommitDeadlockAndCleanupFailure requires
// single-attempt Conflict fallback for rollback/body-cleanup failure, deadlock, connection/unique
// errors, commit rollback and unknown network errors. Private cause text must remain hidden; only
// the dedicated safe serialization path may retry.
// TestArtifactContentReadRetryRejectsUnknownCommitDeadlockAndCleanupFailure требует single-attempt
// Conflict fallback при rollback/body-cleanup failure, deadlock, connection/unique errors, commit
// rollback и unknown network errors. Private cause text остаётся скрытым; retry допустим только
// для выделенного safe serialization path.
func TestArtifactContentReadRetryRejectsUnknownCommitDeadlockAndCleanupFailure(t *testing.T) {
	for _, test := range []struct {
		state           string
		cause, rollback error
		bodyFailed      bool
	}{
		{state: "40001", rollback: errors.New("PRIVATE_ROLLBACK_SENTINEL")},
		{state: "40001", bodyFailed: true},
		{state: "40P01"}, {state: "08006"}, {state: "23505"},
		{cause: pgx.ErrTxCommitRollback}, {cause: errors.New("PRIVATE_NETWORK_SENTINEL")},
	} {
		attempts := 0
		_, err := retryArtifactContentRead(t.Context(), func() (_ platformrepo.ArtifactDownload, resultError error) {
			attempts++
			tx := &artifactContentReadTx{Tx: &artifactReadCleanupStub{rollback: test.rollback}, bodyCleanupFailed: test.bodyFailed}
			defer tx.finish(t.Context(), &resultError)
			cause := test.cause
			if test.state != "" {
				cause = &pgconn.PgError{Code: test.state, Message: "PRIVATE_SQL_SENTINEL"}
			}
			tx.observe(cause)
			return platformrepo.ArtifactDownload{}, errs.ErrConflict
		})
		if attempts != 1 || !errors.Is(err, errs.ErrConflict) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("unknown outcome or failed cleanup retried")
		}
	}
}

// TestArtifactContentReadRetryRefreshesAuthorityAndStopsFreshDenial returns a serialization marker
// once and then a fresh Forbidden/NotFound/Invalid/VersionMismatch, requiring exactly two calls
// and zero partial result. A pre-cancelled context must invoke no attempt; the fake callback
// models fresh authority without verifying a real actor.
// TestArtifactContentReadRetryRefreshesAuthorityAndStopsFreshDenial один раз возвращает
// serialization marker, затем fresh Forbidden/NotFound/Invalid/VersionMismatch, требуя ровно двух
// calls и нулевого partial result. Заранее отменённый context не вызывает attempt; fake callback
// моделирует fresh authority без проверки реального actor.
func TestArtifactContentReadRetryRefreshesAuthorityAndStopsFreshDenial(t *testing.T) {
	for _, denial := range []error{errs.ErrForbidden, errs.ErrNotFound, errs.ErrInvalid, errs.ErrVersionMismatch} {
		attempts := 0
		result, err := retryArtifactContentRead(t.Context(), func() (platformrepo.ArtifactDownload, error) {
			attempts++
			if attempts == 1 {
				return platformrepo.ArtifactDownload{GrantRef: "discarded"}, &artifactReadSerializationRetry{fallback: errs.ErrUnavailable}
			}
			return platformrepo.ArtifactDownload{GrantRef: "not authorized"}, denial
		})
		if attempts != 2 || result.GrantRef != "" || !errors.Is(err, denial) {
			t.Fatal("fresh denial ignored or partial result escaped")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempts := 0
	_, err := retryArtifactContentRead(ctx, func() (platformrepo.ArtifactDownload, error) { attempts++; return platformrepo.ArtifactDownload{}, nil })
	if attempts != 0 || !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("canceled caller started another attempt")
	}
}

// TestArtifactContentReadRetryDoesNotCancelReturnedReaderContext recovers after one synthetic
// serialization marker and requires the caller context to remain live alongside the successful
// grant. It guards lifetime ownership only; no streaming reader or storage object is opened.
// TestArtifactContentReadRetryDoesNotCancelReturnedReaderContext восстанавливается после одного
// synthetic serialization marker и требует сохранения live caller context вместе с successful
// grant. Проверяет только lifetime ownership; streaming reader и storage object не открываются.
func TestArtifactContentReadRetryDoesNotCancelReturnedReaderContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	result, err := retryArtifactContentRead(ctx, func() (platformrepo.ArtifactDownload, error) {
		attempts++
		if attempts == 1 {
			return platformrepo.ArtifactDownload{}, &artifactReadSerializationRetry{fallback: errs.ErrConflict}
		}
		return platformrepo.ArtifactDownload{GrantRef: "committed"}, nil
	})
	if err != nil || attempts != 2 || result.GrantRef != "committed" || ctx.Err() != nil {
		t.Fatal("successful reader lifetime changed")
	}
}
