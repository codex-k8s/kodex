package app

import (
	"errors"
	"testing"
	"time"
)

func TestKubernetesReadinessObserverUsesBoundedLastKnownGood(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	now := startedAt
	observer := newKubernetesReadinessObserver()
	observer.now = func() time.Time { return now }

	available, changed, degraded := observer.Observe(nil, false)
	if !available || changed || degraded {
		t.Fatalf("initial observation = available:%t changed:%t degraded:%t", available, changed, degraded)
	}

	now = startedAt.Add(30 * time.Second)
	available, changed, degraded = observer.Observe(errors.New("transport failure"), true)
	if !available || !changed || !degraded {
		t.Fatalf("first failure = available:%t changed:%t degraded:%t", available, changed, degraded)
	}

	now = startedAt.Add(kubernetesLastKnownGoodWindow - time.Second)
	available, changed, degraded = observer.Observe(errors.New("repeated transport failure"), true)
	if !available || changed || !degraded {
		t.Fatalf("repeated failure = available:%t changed:%t degraded:%t", available, changed, degraded)
	}

	now = startedAt.Add(kubernetesLastKnownGoodWindow)
	available, changed, degraded = observer.Observe(errors.New("window expired"), true)
	if available || changed || !degraded {
		t.Fatalf("expired failure = available:%t changed:%t degraded:%t", available, changed, degraded)
	}
}

func TestKubernetesReadinessObserverRecoveryStartsFreshWindow(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	now := startedAt
	observer := newKubernetesReadinessObserver()
	observer.now = func() time.Time { return now }

	observer.Observe(nil, false)
	now = startedAt.Add(30 * time.Second)
	observer.Observe(errors.New("transport failure"), true)
	now = startedAt.Add(time.Minute)
	available, changed, degraded := observer.Observe(nil, false)
	if !available || !changed || degraded {
		t.Fatalf("recovery = available:%t changed:%t degraded:%t", available, changed, degraded)
	}

	now = startedAt.Add(2*time.Minute + 30*time.Second)
	available, changed, degraded = observer.Observe(errors.New("new transport failure"), true)
	if !available || !changed || !degraded {
		t.Fatalf("new failure = available:%t changed:%t degraded:%t", available, changed, degraded)
	}
}

func TestKubernetesReadinessObserverRejectsLastKnownGoodForIntegrityOrAuthorizationFailure(t *testing.T) {
	t.Parallel()
	observer := newKubernetesReadinessObserver()
	observer.now = func() time.Time { return time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC) }
	observer.Observe(nil, false)

	available, changed, degraded := observer.Observe(errors.New("authorization failure"), false)
	if available || !changed || !degraded {
		t.Fatalf("fail-closed observation = available:%t changed:%t degraded:%t", available, changed, degraded)
	}
}
