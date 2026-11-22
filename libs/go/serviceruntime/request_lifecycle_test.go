package serviceruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// lifecycleContextKey isolates synthetic context values without reusing application identity keys or ambient credentials.
// lifecycleContextKey изолирует synthetic context values без использования application identity keys либо ambient credentials.
type lifecycleContextKey uint8

// TestRequestLifecycleStopAdmissionDoesNotWait proves the shutdown hook can precede a bounded transport stop without treating canceled contexts as completed handlers.
// TestRequestLifecycleStopAdmissionDoesNotWait доказывает, что shutdown hook может предшествовать bounded transport stop без признания canceled contexts завершёнными handlers.
func TestRequestLifecycleStopAdmissionDoesNotWait(t *testing.T) {
	lifecycle, err := NewRequestLifecycle(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	linked, release, err := lifecycle.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if lifecycle.StopAdmission() != nil || linked.Err() != context.Canceled || !lifecycle.Closed() || lifecycle.Joined() {
		t.Fatal("admission stop manufactured handler completion")
	}
	if _, _, err := lifecycle.Begin(t.Context()); !errors.Is(err, ErrActivityGateClosed) {
		t.Fatal("stopped admission accepted another request")
	}
	release()
	if err := lifecycle.Wait(t.Context()); err != nil || !lifecycle.Joined() || lifecycle.StopAdmission() != nil {
		t.Fatal("released handler or removed callback did not join")
	}
}

// TestRequestLifecycleCancelsAndJoinsOwnedRequests distinguishes local context cancellation from actual handler exit, retains upstream request ownership and prevents canceled shutdown from authorizing dependency closure.
// TestRequestLifecycleCancelsAndJoinsOwnedRequests отличает local context cancellation от actual handler exit, сохраняет upstream request ownership и не разрешает dependency closure по canceled shutdown.
func TestRequestLifecycleCancelsAndJoinsOwnedRequests(t *testing.T) {
	application, stopApplication := context.WithCancel(t.Context())
	defer stopApplication()
	lifecycle, err := NewRequestLifecycle(application, 1)
	if err != nil {
		t.Fatal(err)
	}
	incoming := context.WithValue(t.Context(), lifecycleContextKey(1), "request-value")
	linked, release, err := lifecycle.Begin(incoming)
	if err != nil || linked.Value(lifecycleContextKey(1)) != "request-value" {
		t.Fatal("request context was replaced")
	}
	if _, _, err := lifecycle.Begin(incoming); !errors.Is(err, ErrActivityGateLimit) {
		t.Fatal("local bound ignored")
	}
	stopApplication()
	select {
	case <-linked.Done():
	case <-time.After(time.Second):
		t.Fatal("application cancellation did not reach request")
	}
	if incoming.Err() != nil {
		t.Fatal("owned lifecycle canceled upstream request")
	}
	budget, cancelBudget := context.WithCancel(t.Context())
	cancelBudget()
	if !errors.Is(lifecycle.Close(budget), context.Canceled) || lifecycle.Joined() || !lifecycle.Closed() {
		t.Fatal("canceled request became positive handler join")
	}
	if _, _, err := lifecycle.Begin(incoming); !errors.Is(err, ErrActivityGateClosed) {
		t.Fatal("closed lifecycle accepted another handler")
	}
	release()
	release()
	join, cancelJoin := context.WithTimeout(t.Context(), time.Second)
	defer cancelJoin()
	if lifecycle.Wait(join) != nil || !lifecycle.Joined() || lifecycle.Close(join) != nil {
		t.Fatal("actual handler/callback join failed")
	}
}

// TestRequestLifecycleUsesEarlierDeadlineAndIncomingValues proves application cancellation policy cannot overwrite verified request metadata or extend the earlier deadline; completed closure removes its parent callback.
// TestRequestLifecycleUsesEarlierDeadlineAndIncomingValues доказывает, что application cancellation policy не заменяет verified request metadata и не продлевает ранний deadline; completed closure удаляет parent callback.
func TestRequestLifecycleUsesEarlierDeadlineAndIncomingValues(t *testing.T) {
	appDeadline := time.Now().Add(time.Minute)
	application, stopApplication := context.WithDeadline(context.WithValue(t.Context(), lifecycleContextKey(2), "application-only"), appDeadline)
	defer stopApplication()
	incoming, stopIncoming := context.WithDeadline(context.WithValue(t.Context(), lifecycleContextKey(1), "verified-request"), appDeadline.Add(time.Minute))
	defer stopIncoming()
	lifecycle, _ := NewRequestLifecycle(application, 2)
	linked, release, err := lifecycle.Begin(incoming)
	if err != nil {
		t.Fatal(err)
	}
	deadline, exists := linked.Deadline()
	if !exists || !deadline.Equal(appDeadline) || linked.Value(lifecycleContextKey(1)) != "verified-request" || linked.Value(lifecycleContextKey(2)) != nil {
		t.Fatal("lifecycle extended time or injected application authority values")
	}
	release()
	if lifecycle.Close(t.Context()) != nil || !lifecycle.Joined() || incoming.Err() != nil || application.Err() != nil {
		t.Fatal("explicit close failed or canceled caller contexts")
	}
	stopApplication()
	if lifecycle.Wait(t.Context()) != nil {
		t.Fatal("removed callback lost recorded join")
	}
}

// TestRequestLifecycleConcurrentAdmissionAndClose runs the real context callback and simultaneous handler releases under the race detector; every admitted handler is explicitly released before the test ends.
// TestRequestLifecycleConcurrentAdmissionAndClose выполняет actual context callback и simultaneous handler releases под race detector; каждый admitted handler явно освобождается до завершения test.
func TestRequestLifecycleConcurrentAdmissionAndClose(t *testing.T) {
	for range 32 {
		application, cancel := context.WithCancel(t.Context())
		lifecycle, _ := NewRequestLifecycle(application, 4)
		var handlers sync.WaitGroup
		for range 16 {
			handlers.Go(func() {
				_, release, err := lifecycle.Begin(t.Context())
				if err == nil {
					release()
					return
				}
				if !errors.Is(err, ErrActivityGateClosed) && !errors.Is(err, ErrActivityGateLimit) {
					t.Error("unexpected admission error")
				}
			})
		}
		cancel()
		if err := lifecycle.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		handlers.Wait()
		if !lifecycle.Joined() {
			t.Fatal("simultaneous callbacks or handlers did not join")
		}
	}
}

// TestRequestLifecycleRejectsInvalidConstructionAndContexts prevents nil/zero/canceled inputs from reserving work or creating an implicit background owner.
// TestRequestLifecycleRejectsInvalidConstructionAndContexts запрещает nil/zero/canceled inputs резервировать work либо создавать implicit background owner.
func TestRequestLifecycleRejectsInvalidConstructionAndContexts(t *testing.T) {
	if lifecycle, err := NewRequestLifecycle(nil, 1); lifecycle != nil || !errors.Is(err, ErrActivityGateLifecycle) {
		t.Fatal("nil application context accepted")
	}
	if lifecycle, err := NewRequestLifecycle(t.Context(), 0); lifecycle != nil || !errors.Is(err, ErrActivityGateLifecycle) {
		t.Fatal("invalid concurrency accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if lifecycle, err := NewRequestLifecycle(canceled, 1); lifecycle != nil || !errors.Is(err, ErrActivityGateClosed) {
		t.Fatal("canceled application accepted")
	}
	for _, lifecycle := range []*RequestLifecycle{nil, {}} {
		if _, release, err := lifecycle.Begin(t.Context()); release != nil || !errors.Is(err, ErrActivityGateLifecycle) {
			t.Fatal("zero lifecycle admitted handler")
		}
		if !lifecycle.Closed() || lifecycle.Joined() || !errors.Is(lifecycle.StopAdmission(), ErrActivityGateLifecycle) || !errors.Is(lifecycle.Close(t.Context()), ErrActivityGateLifecycle) || !errors.Is(lifecycle.Wait(t.Context()), ErrActivityGateLifecycle) {
			t.Fatal("zero lifecycle manufactured join")
		}
	}
	lifecycle, _ := NewRequestLifecycle(t.Context(), 1)
	if _, release, err := lifecycle.Begin(canceled); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled incoming request admitted")
	}
	if _, release, err := lifecycle.Begin(nil); release != nil || !errors.Is(err, ErrActivityGateLifecycle) {
		t.Fatal("nil incoming request admitted")
	}
	if !errors.Is(lifecycle.Close(nil), ErrActivityGateLifecycle) || !errors.Is(lifecycle.Wait(nil), ErrActivityGateLifecycle) {
		t.Fatal("nil join observer accepted")
	}
	if lifecycle.Close(t.Context()) != nil || !lifecycle.Joined() {
		t.Fatal("unused lifecycle retained owned callback")
	}
}
