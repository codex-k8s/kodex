package serviceruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestStartupProbeKeepsBudgetAndOwnership verifies refusal recovery, terminal precedence and cancellation without invoking cleanup or business effects.
// TestStartupProbeKeepsBudgetAndOwnership проверяет восстановление после отказа, приоритет постоянного отказа и отмену без cleanup либо бизнесовых эффектов.
func TestStartupProbeKeepsBudgetAndOwnership(t *testing.T) {
	for _, scenario := range []string{"success", "recover", "terminal", "later-terminal", "cancel-before", "cancel-after", "cancel-wait", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			if scenario == "cancel-before" {
				cancel()
			}
			calls := 0
			retry := func(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
			failed := WaitStartupProbe(ctx, func(probe context.Context) error {
				calls++
				d, ok := probe.Deadline()
				if !ok || time.Until(d) > 20*time.Millisecond {
					t.Fatal("probe budget exceeded")
				}
				switch scenario {
				case "recover":
					if calls < 3 {
						return syscall.ECONNREFUSED
					}
				case "terminal":
					return syscall.EACCES
				case "later-terminal":
					if calls == 1 {
						return syscall.ECONNREFUSED
					}
					return syscall.EACCES
				case "cancel-after":
					cancel()
					return nil
				case "cancel-wait":
					cancel()
					return syscall.ECONNREFUSED
				case "deadline":
					<-probe.Done()
					return probe.Err()
				}
				return nil
			}, retry, 20*time.Millisecond, time.Millisecond)
			switch scenario {
			case "success", "recover":
				want := 1
				if scenario == "recover" {
					want = 3
				}
				if failed != nil || calls != want || ctx.Err() != nil {
					t.Fatal("successful admission changed")
				}
			case "terminal", "later-terminal":
				want := 1
				if scenario == "later-terminal" {
					want = 2
				}
				if failed == nil || !errors.Is(failed.Cause, syscall.EACCES) || failed.Budget != nil || calls != want {
					t.Fatal("permanent error retried or hidden")
				}
			case "cancel-before":
				if failed == nil || !errors.Is(failed.Budget, context.Canceled) || calls != 0 {
					t.Fatal("canceled caller probed")
				}
			case "cancel-after", "cancel-wait":
				if failed == nil || !errors.Is(failed.Budget, context.Canceled) || calls != 1 {
					t.Fatal("canceled probe admitted or retried")
				}
			case "deadline":
				if failed == nil || !errors.Is(failed.Cause, context.DeadlineExceeded) || calls != 1 {
					t.Fatal("probe timeout retried")
				}
			}
		})
	}
}

// TestStartupProbeCancellationInterruptsPause preserves the last refusal and ensures a second probe never starts after cancellation.
// TestStartupProbeCancellationInterruptsPause сохраняет последний отказ и исключает второй ping после отмены в паузе.
func TestStartupProbeCancellationInterruptsPause(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var calls atomic.Int32
	probed := make(chan struct{})
	done := make(chan *StartupProbeFailure, 1)
	go func() {
		done <- WaitStartupProbe(ctx, func(context.Context) error { calls.Add(1); close(probed); return syscall.ECONNREFUSED }, func(error) bool { return true }, time.Second, time.Second)
	}()
	<-probed
	cancel()
	select {
	case failed := <-done:
		if failed == nil || !errors.Is(failed.Cause, syscall.ECONNREFUSED) || !errors.Is(failed.Budget, context.Canceled) || calls.Load() != 1 {
			t.Fatal("refusal or cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("pause ignored cancellation")
	}
}

// TestStartupProbeRequiresBoundedRetry refuses to loop with an unbounded caller even if the service predicate permits retry.
// TestStartupProbeRequiresBoundedRetry запрещает цикл при неограниченном caller context, даже если сервисный предикат допускает retry.
func TestStartupProbeRequiresBoundedRetry(t *testing.T) {
	calls := 0
	result := WaitStartupProbe(context.Background(), func(context.Context) error { calls++; return syscall.ECONNREFUSED }, func(error) bool { return true }, time.Millisecond, time.Millisecond)
	if result == nil || calls != 1 || !errors.Is(result.Cause, syscall.ECONNREFUSED) {
		t.Fatal("unbounded retry admitted")
	}
}

// TestStartupProbeDoesNotHidePermanentErrorJoinedWithCancellation retains the new failure separately from the previous retry for service-owned classification.
// TestStartupProbeDoesNotHidePermanentErrorJoinedWithCancellation сохраняет новый отказ отдельно от прежнего retry для сервисной классификации.
func TestStartupProbeDoesNotHidePermanentErrorJoinedWithCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	calls := 0
	terminal := errors.Join(syscall.EACCES, context.Canceled)
	result := WaitStartupProbe(ctx, func(context.Context) error {
		calls++
		if calls == 1 {
			return syscall.ECONNREFUSED
		}
		cancel()
		return terminal
	}, func(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }, time.Second, time.Millisecond)
	if result == nil || result.Cause != terminal || !errors.Is(result.PreviousRetry, syscall.ECONNREFUSED) || !errors.Is(result.Budget, context.Canceled) || calls != 2 {
		t.Fatal("current failure concealed by previous retry")
	}
}
