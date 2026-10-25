package app

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// TestInitialNATSWaitRetriesOnlyNoServersAndTransfersOwnership exercises two
// transient attempts and one success without a real broker. Each failed partial
// resource is released once, while accepted ownership stays with the caller.
// TestInitialNATSWaitRetriesOnlyNoServersAndTransfersOwnership проверяет две
// временные попытки и успех без broker. Каждый частичный ресурс ошибки закрывается
// ровно один раз; принятый ресурс остаётся во владении вызывающего.
func TestInitialNATSWaitRetriesOnlyNoServersAndTransfersOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	attempts, releases := 0, 0
	err := waitForInitialNATS(ctx, time.Millisecond, func() (func(), error) {
		attempts++
		release := func() { releases++ }
		if attempts < 3 {
			return release, natsStartupFailure(natsStageConnect, nats.ErrNoServers)
		}
		return release, nil
	})
	if err != nil || attempts != 3 || releases != 2 {
		t.Fatal("transient retry changed resource ownership or cardinality")
	}
}

// TestInitialNATSWaitPermanentFailureRunsOnce checks actual closed classification
// rather than error text: permanent causes override joined no_servers, and flush,
// unknown, unmarked or previously exhausted failures never authorize another call.
// TestInitialNATSWaitPermanentFailureRunsOnce проверяет закрытую классификацию,
// а не текст: permanent-причина важнее joined no_servers, а flush, unknown,
// unmarked и ранее исчерпанные ошибки не разрешают следующий вызов.
func TestInitialNATSWaitPermanentFailureRunsOnce(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure error
	}{
		{"external wrapper", fmt.Errorf("PRIVATE_OUTER: %w", natsStartupFailure(natsStageConnect, nats.ErrNoServers))},
		{"external configuration right", errors.Join(natsStartupFailure(natsStageConnect, nats.ErrNoServers), natsStartupFailure(natsStageLoadCredentials, unreadableDiagnosticCause{}))},
		{"external configuration left", errors.Join(natsStartupFailure(natsStageLoadCredentials, unreadableDiagnosticCause{}), natsStartupFailure(natsStageConnect, nats.ErrNoServers))},
		{"external flush right", errors.Join(natsStartupFailure(natsStageConnect, nats.ErrNoServers), natsStartupFailure(natsStageVerify, nats.ErrNoServers))},
		{"external flush left", errors.Join(natsStartupFailure(natsStageVerify, nats.ErrNoServers), natsStartupFailure(natsStageConnect, nats.ErrNoServers))},
		{"external budget right", errors.Join(natsStartupFailure(natsStageConnect, nats.ErrNoServers), natsStartupBudgetFailure(nats.ErrNoServers, context.DeadlineExceeded))},
		{"external budget left", errors.Join(natsStartupBudgetFailure(nats.ErrNoServers, context.DeadlineExceeded), natsStartupFailure(natsStageConnect, nats.ErrNoServers))},
		{"nested diagnostic", &natsStartupError{stage: natsStageConnect, cause: natsStartupFailure(natsStageConnect, nats.ErrNoServers)}},
		{"external sibling auth", errors.Join(natsStartupFailure(natsStageConnect, nats.ErrNoServers), nats.ErrAuthorization)},
		{"external sibling cancel", errors.Join(natsStartupFailure(natsStageConnect, nats.ErrNoServers), context.Canceled)},
		{"auth", natsStartupFailure(natsStageConnect, errors.Join(nats.ErrNoServers, nats.ErrAuthorization))},
		{"TLS", natsStartupFailure(natsStageConnect, errors.Join(nats.ErrNoServers, x509.UnknownAuthorityError{}))},
		{"configuration", natsStartupFailure(natsStageLoadCredentials, nats.ErrNoServers)},
		{"flush", natsStartupFailure(natsStageVerify, nats.ErrNoServers)},
		{"unknown", natsStartupFailure(natsStageConnect, unreadableDiagnosticCause{})},
		{"raw no_servers", nats.ErrNoServers},
		{"prior budget", natsStartupBudgetFailure(natsStartupFailure(natsStageConnect, nats.ErrNoServers), context.DeadlineExceeded)},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts, releases := 0, 0
			err := waitForInitialNATS(t.Context(), time.Hour, func() (func(), error) { attempts++; return func() { releases++ }, test.failure })
			if !errors.Is(err, test.failure) || attempts != 1 || releases != 1 {
				t.Fatal("permanent or non-connect failure retried or leaked a partial resource")
			}
		})
	}
}

// TestInitialNATSWaitDeadlineKeepsCauseAndStopsAdmission uses a real deadline
// during a long pause, retaining both the no_servers identity and budget outcome
// without admitting another attempt or formatting private causes.
// TestInitialNATSWaitDeadlineKeepsCauseAndStopsAdmission использует настоящий
// deadline во время длинной паузы, сохраняя no_servers и исход ожидания без новой
// попытки либо форматирования приватных причин.
func TestInitialNATSWaitDeadlineKeepsCauseAndStopsAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	attempts := 0
	err := waitForInitialNATS(ctx, time.Hour, func() (func(), error) {
		attempts++
		return nil, natsStartupFailure(natsStageConnect, nats.ErrNoServers)
	})
	if attempts != 1 || !errors.Is(err, nats.ErrNoServers) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline lost final cause or admitted a new attempt")
	}
	want := "connect realtime NATS consumer error_class=no_servers wait_status=deadline"
	for _, format := range []string{"%v", "%+v", "%#v", "%q"} {
		if fmt.Sprintf(format, err) != want {
			t.Fatal("budget diagnostic exposed private fields or lost outcome")
		}
	}
}

// TestInitialNATSWaitCancellationBeforeAndDuringPause joins its test goroutine
// and checks cancellation before any attempt and after partial cleanup without
// allowing an additional factory call. Production itself starts no goroutine.
// TestInitialNATSWaitCancellationBeforeAndDuringPause дожидается тестовой
// goroutine и проверяет отмену до первой попытки и после частичной очистки без
// следующего вызова factory. Production не создаёт goroutine.
func TestInitialNATSWaitCancellationBeforeAndDuringPause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempts := 0
	err := waitForInitialNATS(ctx, time.Hour, func() (func(), error) { attempts++; return nil, nil })
	if attempts != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled context attempted a connection")
	}
	paused, cancelPause := context.WithCancel(t.Context())
	defer cancelPause()
	releasing, resume := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	joined := make(chan struct{})
	var resumeOnce sync.Once
	resumeAttempt := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(func() {
		cancelPause()
		resumeAttempt()
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Error("test waiter did not join cleanup")
		}
	})
	var calls atomic.Int32
	go func() {
		defer close(joined)
		done <- waitForInitialNATS(paused, time.Hour, func() (func(), error) {
			calls.Add(1)
			return func() { close(releasing); <-resume }, natsStartupFailure(natsStageConnect, nats.ErrNoServers)
		})
	}()
	select {
	case <-releasing:
	case <-time.After(time.Second):
		t.Fatal("partial cleanup did not start")
	}
	cancelPause()
	resumeAttempt()
	select {
	case err = <-done:
		if calls.Load() != 1 || !errors.Is(err, context.Canceled) || !errors.Is(err, nats.ErrNoServers) || !strings.HasSuffix(err.Error(), " wait_status=canceled") {
			t.Fatal("pause cancellation lost cause or admitted a new attempt")
		}
	case <-time.After(time.Second):
		t.Fatal("pause cancellation did not join")
	}
}

// TestInitialNATSWaitRejectsLateSuccessAndPreservesLaterPermanentCause models
// cancellation inside a synchronous probe. Late success is released before
// return, and later auth evidence is never replaced by an earlier no_servers.
// TestInitialNATSWaitRejectsLateSuccessAndPreservesLaterPermanentCause моделирует
// отмену внутри синхронного probe. Поздний успех закрывается до возврата, а более
// поздняя auth-причина не заменяется предыдущим no_servers.
func TestInitialNATSWaitRejectsLateSuccessAndPreservesLaterPermanentCause(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		attempts, releases := 0, 0
		err := waitForInitialNATS(ctx, time.Millisecond, func() (func(), error) {
			attempts++
			if attempts == 1 {
				return nil, natsStartupFailure(natsStageConnect, nats.ErrNoServers)
			}
			cancel()
			var result error
			if permanent {
				result = natsStartupFailure(natsStageConnect, nats.ErrAuthorization)
			}
			return func() { releases++ }, result
		})
		cancel()
		if attempts != 2 || releases != 1 || !errors.Is(err, context.Canceled) {
			t.Fatal("late outcome bypassed startup ownership or cancellation")
		}
		if permanent {
			if !errors.Is(err, nats.ErrAuthorization) || !strings.Contains(err.Error(), "error_class=authorization wait_status=canceled") {
				t.Fatal("earlier transient hid later permanent failure")
			}
		} else if !errors.Is(err, nats.ErrNoServers) {
			t.Fatal("late success lost the last failed attempt")
		}
	}
}

// TestInitialNATSWaitClosesSuccessAfterDeadline waits for a real deadline inside
// the synchronous test constructor, then verifies its late resource is closed
// once and cannot be accepted as a startup success.
// TestInitialNATSWaitClosesSuccessAfterDeadline ждёт настоящий deadline внутри
// синхронного тестового конструктора, затем проверяет однократное закрытие позднего
// ресурса без принятия его как startup success.
func TestInitialNATSWaitClosesSuccessAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	attempts, releases := 0, 0
	err := waitForInitialNATS(ctx, time.Hour, func() (func(), error) {
		attempts++
		<-ctx.Done()
		return func() { releases++ }, nil
	})
	if attempts != 1 || releases != 1 || !errors.Is(err, context.DeadlineExceeded) ||
		err.Error() != "connect realtime NATS consumer error_class=timeout wait_status=deadline" {
		t.Fatal("expired success bypassed close-before-handoff")
	}
}

// TestInitialNATSWaitDoesNotParseTextOrInventBudget checks typed closure under
// misleading text, joined errors and unknown budget without a real dependency.
// TestInitialNATSWaitDoesNotParseTextOrInventBudget проверяет закрытую типизацию
// при вводящем в заблуждение тексте, joined errors и unknown budget без dependency.
func TestInitialNATSWaitDoesNotParseTextOrInventBudget(t *testing.T) {
	failure := natsStartupFailure(natsStageConnect, errors.New("no_servers connection refused PRIVATE"))
	if initialNATSRetryable(failure) {
		t.Fatal("raw text authorized retry")
	}
	unknown := natsStartupBudgetFailure(unreadableDiagnosticCause{}, unreadableDiagnosticCause{})
	if unknown.Error() != "connect realtime NATS consumer error_class=unknown wait_status=unknown" {
		t.Fatal("unknown outcome escaped the closed registry")
	}
	if natsFailureClass(natsStageConnect, syscall.ECONNREFUSED) != "connection_refused" || initialNATSRetryable(natsStartupFailure(natsStageConnect, syscall.ECONNREFUSED)) {
		t.Fatal("retry policy widened from no_servers to network failures")
	}
}
