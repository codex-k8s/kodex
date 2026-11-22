package serviceruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestActivityGateClosesAdmissionAndJoinsReleases proves that cancellation, overload and admission closure do not replace actual activity release; a recorded join remains a fact even under a canceled observer context.
// TestActivityGateClosesAdmissionAndJoinsReleases доказывает, что cancellation, overload и admission closure не заменяют actual activity release; recorded join остаётся фактом также при canceled observer context.
func TestActivityGateClosesAdmissionAndJoinsReleases(t *testing.T) {
	gate, err := NewActivityGate(1)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Closed() || gate.Joined() || !errors.Is(gate.Wait(t.Context()), ErrActivityGateLifecycle) {
		t.Fatal("open admission appeared joined")
	}
	release, err := gate.Enter()
	if err != nil {
		t.Fatal(err)
	}
	if next, err := gate.Enter(); next != nil || !errors.Is(err, ErrActivityGateLimit) {
		t.Fatal("local concurrency bound ignored")
	}
	if gate.Close() != nil || gate.Close() != nil || !gate.Closed() || gate.Joined() {
		t.Fatal("closed admission falsely joined activity")
	}
	if next, err := gate.Enter(); next != nil || !errors.Is(err, ErrActivityGateClosed) {
		t.Fatal("closed admission accepted work")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(gate.Wait(ctx), context.Canceled) || gate.Joined() {
		t.Fatal("observation cancellation manufactured join")
	}
	release()
	release()
	if !gate.Joined() || gate.Wait(ctx) != nil {
		t.Fatal("actual release failed positive join")
	}
}

// TestActivityGateConcurrentReleaseIsIdempotent joins all real release callers before observing the gate; duplicate releases cannot underflow activity accounting or close its channel twice.
// TestActivityGateConcurrentReleaseIsIdempotent ожидает всех actual release callers до наблюдения gate; duplicate releases не уменьшают счётчик ниже нуля и не закрывают channel дважды.
func TestActivityGateConcurrentReleaseIsIdempotent(t *testing.T) {
	gate, _ := NewActivityGate(4)
	first, _ := gate.Enter()
	second, _ := gate.Enter()
	if gate.Close() != nil {
		t.Fatal("close failed")
	}
	var callers sync.WaitGroup
	for range 32 {
		callers.Go(func() { first() })
	}
	callers.Wait()
	if gate.Joined() {
		t.Fatal("first activity releases concealed second activity")
	}
	second()
	if gate.Wait(t.Context()) != nil || !gate.Joined() {
		t.Fatal("all activity releases did not join")
	}
}

// TestActivityGateRejectsInvalidLifecycle keeps nil and zero values unavailable rather than panicking or presenting quiescence; construction starts no activity that would require cleanup.
// TestActivityGateRejectsInvalidLifecycle сохраняет nil и zero values недоступными вместо panic либо объявления quiescence; construction не запускает activity, требующую cleanup.
func TestActivityGateRejectsInvalidLifecycle(t *testing.T) {
	for _, maximum := range []int{-1, 0, 129} {
		if gate, err := NewActivityGate(maximum); gate != nil || !errors.Is(err, ErrActivityGateLifecycle) {
			t.Fatal("invalid local bound accepted")
		}
	}
	for _, gate := range []*ActivityGate{nil, {}} {
		if release, err := gate.Enter(); release != nil || !errors.Is(err, ErrActivityGateLifecycle) {
			t.Fatal("invalid gate admitted activity")
		}
		if !gate.Closed() || gate.Joined() || !errors.Is(gate.Close(), ErrActivityGateLifecycle) || !errors.Is(gate.Wait(t.Context()), ErrActivityGateLifecycle) {
			t.Fatal("invalid gate manufactured lifecycle facts")
		}
	}
	gate, _ := NewActivityGate(1)
	_ = gate.Close()
	if !errors.Is(gate.Wait(nil), ErrActivityGateLifecycle) {
		t.Fatal("nil observer accepted")
	}
}
