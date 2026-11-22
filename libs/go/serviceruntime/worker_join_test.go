package serviceruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWorkerGroupJoinRequiresActualCollector distinguishes cancellation, Stop and wait timeout from the joined error result without adding a waiter goroutine or claiming remote process quiescence.
// TestWorkerGroupJoinRequiresActualCollector отличает cancellation, Stop и wait timeout от joined error result без waiter goroutine либо утверждений remote process quiescence.
func TestWorkerGroupJoinRequiresActualCollector(t *testing.T) {
	unblock := make(chan struct{})
	failure := errors.New("synthetic joined worker failure")
	group := StartWorkers(t.Context(), func(context.Context) error { <-unblock; return failure })
	if group.Joined() {
		t.Fatal("live worker declared joined")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if group.Wait(cancelled) == nil || group.Joined() {
		t.Fatal("cancelled wait became join")
	}
	group.Stop()
	if group.Joined() {
		t.Fatal("Stop became join without worker exit")
	}
	close(unblock)
	bounded, done := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
	defer done()
	if err := group.Wait(bounded); !errors.Is(err, failure) || !group.Joined() {
		t.Fatal("joined failure lost positive collector proof")
	}
	if (*WorkerGroup)(nil).Joined() || (&WorkerGroup{}).Joined() {
		t.Fatal("absent collector became joined")
	}
}
