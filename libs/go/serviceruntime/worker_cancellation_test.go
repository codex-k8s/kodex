package serviceruntime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestWorkersPreserveFailureJoinedWithCancellation uses the real collector to reject cancellation masking of a distinct joined failure, while retaining ordinary cancellation behavior.
// TestWorkersPreserveFailureJoinedWithCancellation использует real collector и запрещает masking distinct joined failure через cancellation, сохраняя ordinary cancellation behavior.
func TestWorkersPreserveFailureJoinedWithCancellation(t *testing.T) {
	failure := errors.New("synthetic fatal cleanup failure")
	for _, sample := range []struct {
		name        string
		workerError error
		fatal       bool
	}{
		{"plain", context.Canceled, false},
		{"wrapped", fmt.Errorf("worker stopped: %w", context.Canceled), false},
		{"joined-cancellation", errors.Join(context.Canceled, fmt.Errorf("worker stopped: %w", context.Canceled)), false},
		{"joined-fatal", errors.Join(context.Canceled, failure), true},
		{"wrapped-joined-fatal", fmt.Errorf("worker stopped: %w", errors.Join(context.Canceled, failure)), true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			group := StartWorkers(t.Context(), func(context.Context) error { return sample.workerError })
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
			defer cancel()
			result := group.Wait(ctx)
			if !group.Joined() {
				t.Fatal("collector did not join")
			}
			if sample.fatal {
				if !errors.Is(result, failure) {
					t.Fatal("cancellation hid fatal worker error")
				}
			} else if result != nil {
				t.Fatal("ordinary cancellation became failure")
			}
		})
	}
}

// cancellationCycleFixture models a broken SDK unwrap cycle without runtime logs or sensitive fields.
// cancellationCycleFixture моделирует broken SDK unwrap cycle без runtime logs либо sensitive fields.
type cancellationCycleFixture struct{}

// Error returns a fixed fixture diagnostic without recursively formatting its cause.
// Error возвращает fixed fixture diagnostic без recursive formatting cause.
func (cancellationCycleFixture) Error() string { return "synthetic cancellation unwrap cycle" }

// Unwrap deliberately cycles to prove classifier traversal stays bounded.
// Unwrap намеренно образует cycle для проверки bounded traversal classifier.
func (cancellationCycleFixture) Unwrap() error { return cancellationCycleFixture{} }

// TestWorkerCancellationClassifierClosesMalformedTrees checks cycles and empty joins cannot establish ordinary cancellation.
// TestWorkerCancellationClassifierClosesMalformedTrees проверяет, что cycles и empty joins не устанавливают ordinary cancellation.
func TestWorkerCancellationClassifierClosesMalformedTrees(t *testing.T) {
	causes := make([]error, 65)
	for index := range causes {
		causes[index] = context.Canceled
	}
	if workerCancellationOnly(errors.Join(causes...), 64) {
		t.Fatal("oversized unwrap tree escaped shared node budget")
	}

	if workerCancellationOnly(cancellationCycleFixture{}, 64) || workerCancellationOnly(nil, 64) {
		t.Fatal("malformed unwrap tree became ordinary cancellation")
	}
}
