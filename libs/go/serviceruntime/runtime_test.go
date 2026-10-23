package serviceruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWorkersCancelAndJoin verifies that parent cancellation reaches the worker
// and its ordinary cancellation does not become a group failure after joining.
// TestWorkersCancelAndJoin проверяет доставку отмены родителя worker и отсутствие
// ошибки группы после join при обычном context.Canceled.
func TestWorkersCancelAndJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	group := StartWorkers(ctx, func(workerContext context.Context) error {
		<-workerContext.Done()
		return workerContext.Err()
	})
	cancel()
	waitContext, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := group.Wait(waitContext); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

// TestShutdownUsesIndependentContexts preserves the first cleanup error while
// requiring the next callback to receive a new, still-live context.
// TestShutdownUsesIndependentContexts сохраняет первую cleanup-ошибку и требует
// нового ещё живого контекста для следующего callback.
func TestShutdownUsesIndependentContexts(t *testing.T) {
	first := errors.New("first")
	secondRan := false
	err := RunShutdown(
		context.Background(),
		ShutdownOperation{
			Name:    "first",
			Timeout: time.Second,
			Run: func(context.Context) error {
				return first
			},
		},
		ShutdownOperation{
			Name:    "second",
			Timeout: time.Second,
			Run: func(ctx context.Context) error {
				secondRan = ctx.Err() == nil
				return nil
			},
		},
	)
	if !errors.Is(err, first) || !secondRan {
		t.Fatalf("shutdown result = %v, second=%v", err, secondRan)
	}
}

// TestReadinessSnapshotAndTransitionEdge checks the initial closed state,
// one true transition edge and suppression of an unchanged repeated snapshot.
// TestReadinessSnapshotAndTransitionEdge проверяет начальную неготовность,
// единственный edge изменения и подавление повторного неизменённого снимка.
func TestReadinessSnapshotAndTransitionEdge(t *testing.T) {
	t.Parallel()

	readiness := NewReadiness()
	if ready, reason := readiness.Ready(); ready || reason != "starting" {
		t.Fatalf("initial readiness = %t %q", ready, reason)
	}
	if !readiness.Set(true, "ready") || readiness.Set(true, "ready") {
		t.Fatal("readiness transition edge is incorrect")
	}
	if ready, reason := readiness.Ready(); !ready || reason != "ready" {
		t.Fatalf("updated readiness = %t %q", ready, reason)
	}
}
