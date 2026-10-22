package callback

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/testfixture"
)

// TestCoordinatorMatchesWarmExecutionByCompatibilityDigest enqueues one local fixture
// and verifies a canceled wait for a different compatibility key returns no execution.
// It proves local matching, not a durable claim or distributed scheduler decision.
// TestCoordinatorMatchesWarmExecutionByCompatibilityDigest добавляет local fixture
// и проверяет отсутствие execution у canceled wait другого compatibility key.
// Проверяется local matching,не durable claim и не distributed scheduler decision.
func TestCoordinatorMatchesWarmExecutionByCompatibilityDigest(t *testing.T) {
	t.Parallel()
	coordinator := NewCoordinator()
	input := validWarmExecutionInput()
	compatibility := strings.Repeat("a", 64)
	if err := coordinator.EnqueueWarm(input, compatibility); err != nil {
		t.Fatalf("EnqueueWarm() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, available := coordinator.NextWarm(ctx, strings.Repeat("b", 64)); available {
		t.Fatal("incompatible warm runtime received execution")
	}
}

// TestCoordinatorReturnsCompatibleWarmExecution checks a matching local compatibility
// key returns the originally queued lease without altering its synthetic input.
// TestCoordinatorReturnsCompatibleWarmExecution проверяет возврат исходного queued lease
// по matching local compatibility key без изменения synthetic input.
func TestCoordinatorReturnsCompatibleWarmExecution(t *testing.T) {
	t.Parallel()
	coordinator := NewCoordinator()
	input := validWarmExecutionInput()
	compatibility := strings.Repeat("a", 64)
	if err := coordinator.EnqueueWarm(input, compatibility); err != nil {
		t.Fatalf("EnqueueWarm() error = %v", err)
	}
	result, available := coordinator.NextWarm(t.Context(), compatibility)
	if !available || result.LeaseRef != input.LeaseRef {
		t.Fatalf("NextWarm() = %#v, %t", result, available)
	}
}

// validWarmExecutionInput builds a complete synthetic turn/environment/image/model/
// credential-path snapshot and derives its input/binding digests for local unit tests.
// Refs, credential hashes and mount paths are fixture metadata only, not real secrets,
// owner grants or proof that an image or provider session exists.
// validWarmExecutionInput создаёт полный synthetic turn/environment/image/model/
// credential-path snapshot и считает input/binding digests local unit tests.
// Refs,credential hashes,mount paths — fixture metadata,не реальные secrets,
// owner grants и не доказательство существования image/provider session.
func validWarmExecutionInput() runtimecontract.RunnerInput {
	return testfixture.TurnInput()
}
