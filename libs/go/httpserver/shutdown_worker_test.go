package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestShutdownWorkerConstructionIsLazy checks that assembling the adapter invokes neither callback.
// It reserves no socket and starts no service or shutdown operation.
// TestShutdownWorkerConstructionIsLazy проверяет, что создание адаптера не вызывает ни один callback.
// Сокет не резервируется, сервис и операция остановки не запускаются.
func TestShutdownWorkerConstructionIsLazy(t *testing.T) {
	var calls atomic.Int32
	worker := ShutdownWorker(func() error { calls.Add(1); return nil }, func(context.Context) error { calls.Add(1); return nil }, time.Second)
	if worker == nil || calls.Load() != 0 {
		t.Fatal("shutdown worker construction invoked a callback")
	}
}

// TestShutdownWorkerPreservesServeResult covers immediate success, normal/wrapped HTTP closure and unexpected serve failure.
// Returning serve results must not invoke shutdown or replace an unexpected error's identity.
// TestShutdownWorkerPreservesServeResult охватывает немедленный успех, штатное/обёрнутое закрытие HTTP и неожиданную ошибку обслуживания.
// Возврат результата обслуживания не должен вызывать остановку или подменять идентичность неожиданной ошибки.
func TestShutdownWorkerPreservesServeResult(t *testing.T) {
	failure := errors.New("synthetic serve failure")
	for _, test := range []struct {
		name  string
		serve error
		want  error
	}{
		{"success", nil, nil},
		{"closed", http.ErrServerClosed, nil},
		{"wrapped closed", fmt.Errorf("synthetic closure: %w", http.ErrServerClosed), nil},
		{"failure", failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			var shutdownCalls atomic.Int32
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			worker := ShutdownWorker(func() error { return test.serve }, func(context.Context) error { shutdownCalls.Add(1); return nil }, time.Second)
			if err := worker(ctx); err != test.want || shutdownCalls.Load() != 0 {
				t.Fatalf("serve result changed: error=%v shutdown_calls=%d", err, shutdownCalls.Load())
			}
		})
	}
}

// TestShutdownWorkerCancellationJoinsAndPreservesCauses uses controlled callbacks to force the cancellation branch.
// Shutdown retains context values but not the parent's cancellation/deadline; serve is joined and both failures survive, while normal HTTP closure is ignored.
// TestShutdownWorkerCancellationJoinsAndPreservesCauses управляемыми callbacks принудительно выбирает ветку отмены.
// Остановка сохраняет значения контекста, но не родительскую отмену/срок; обслуживание ожидается и обе ошибки сохраняются, а штатное закрытие HTTP игнорируется.
func TestShutdownWorkerCancellationJoinsAndPreservesCauses(t *testing.T) {
	serveFailure := errors.New("synthetic serve failure")
	shutdownFailure := errors.New("synthetic shutdown failure")
	parentFailure := errors.New("synthetic parent cancellation")
	for _, shutdownErr := range []error{nil, shutdownFailure} {
		for _, test := range []struct {
			name  string
			serve error
		}{
			{"success", nil},
			{"closed", http.ErrServerClosed},
			{"wrapped closed", fmt.Errorf("synthetic closure: %w", http.ErrServerClosed)},
			{"failure", serveFailure},
		} {
			t.Run(fmt.Sprintf("%s/shutdown_failure_%t", test.name, shutdownErr != nil), func(t *testing.T) {
				key := struct{ name string }{"shutdown-context"}
				parentDeadline := time.Now().Add(100 * time.Millisecond)
				parent, cancelDeadline := context.WithDeadline(context.WithValue(t.Context(), key, "retained"), parentDeadline)
				defer cancelDeadline()
				ctx, cancel := context.WithCancelCause(parent)
				defer cancel(parentFailure)
				entered, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				releaseServe := func() { releaseOnce.Do(func() { close(release) }) }
				defer releaseServe()
				observed := make(chan context.Context, 1)
				var serveCalls, shutdownCalls atomic.Int32
				worker := ShutdownWorker(func() error {
					serveCalls.Add(1)
					close(entered)
					<-release
					return test.serve
				}, func(shutdown context.Context) error {
					shutdownCalls.Add(1)
					observed <- shutdown
					return shutdownErr
				}, 2*time.Second)
				finished := make(chan error, 1)
				go func() { finished <- worker(ctx) }()
				awaitWorkerSignal(t, entered)
				cancel(parentFailure)
				var shutdownContext context.Context
				select {
				case shutdownContext = <-observed:
				case <-time.After(time.Second):
					t.Fatal("shutdown callback was not invoked")
				}
				deadline, bounded := shutdownContext.Deadline()
				if shutdownContext.Err() != nil || context.Cause(shutdownContext) != nil || shutdownContext.Value(key) != "retained" || !bounded || !deadline.After(parentDeadline) {
					t.Fatal("shutdown inherited parent cancellation/deadline or lost values")
				}
				select {
				case <-finished:
					t.Fatal("shutdown worker returned before joining serve")
				default:
				}
				releaseServe()
				var result error
				select {
				case result = <-finished:
				case <-time.After(time.Second):
					t.Fatal("shutdown worker did not join serve")
				}
				if (shutdownErr != nil && !errors.Is(result, shutdownErr)) || (test.serve == serveFailure && !errors.Is(result, serveFailure)) ||
					(shutdownErr == nil && test.serve != serveFailure && result != nil) || errors.Is(result, http.ErrServerClosed) ||
					serveCalls.Load() != 1 || shutdownCalls.Load() != 1 || !errors.Is(shutdownContext.Err(), context.Canceled) {
					t.Fatalf("shutdown or serve result changed: %v", result)
				}
			})
		}
	}
}

// TestShutdownWorkerTimeoutRemainsBounded makes shutdown wait for its own deadline, then releases serve.
// The worker must preserve DeadlineExceeded after joining instead of returning the earlier parent cancellation.
// TestShutdownWorkerTimeoutRemainsBounded заставляет остановку ждать собственного срока, затем освобождает обслуживание.
// Работник должен сохранить DeadlineExceeded после ожидания, а не вернуть более раннюю отмену родителя.
func TestShutdownWorkerTimeoutRemainsBounded(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	worker := ShutdownWorker(func() error {
		close(entered)
		<-release
		return http.ErrServerClosed
	}, func(ctx context.Context) error {
		defer close(release)
		<-ctx.Done()
		return ctx.Err()
	}, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- worker(ctx) }()
	awaitWorkerSignal(t, entered)
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown timeout classification changed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown worker exceeded its test budget")
	}
}
