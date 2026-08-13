package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type snapshotMaintenanceStub struct {
	readyErr      error
	activationErr error
	readyCalls    int
	activations   int
}

func (stub *snapshotMaintenanceStub) ServedStateReady(context.Context) error {
	stub.readyCalls++
	return stub.readyErr
}

func (stub *snapshotMaintenanceStub) ActivateSnapshot(context.Context) error {
	stub.activations++
	return stub.activationErr
}

func TestMaintainServedSnapshotUsesExistingReadbackBeforeRefresh(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	lastRefresh := now.Add(-snapshotReadbackRefreshInterval / 2)
	stub := &snapshotMaintenanceStub{}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, lastRefresh, now)
	if err != nil {
		t.Fatalf("maintain served snapshot: %v", err)
	}
	if refreshedAt != lastRefresh || stub.readyCalls != 1 || stub.activations != 0 {
		t.Fatalf("unexpected maintenance result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

func TestMaintainServedSnapshotRefreshesBeforeReceiptExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	stub := &snapshotMaintenanceStub{}

	refreshedAt, err := maintainServedSnapshot(
		context.Background(),
		stub,
		now.Add(-snapshotReadbackRefreshInterval),
		now,
	)
	if err != nil {
		t.Fatalf("refresh served snapshot: %v", err)
	}
	if refreshedAt != now || stub.readyCalls != 0 || stub.activations != 1 {
		t.Fatalf("unexpected refresh result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

func TestMaintainServedSnapshotRecoversRejectedReadbackImmediately(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	stub := &snapshotMaintenanceStub{readyErr: errors.New("receipt expired")}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, now, now.Add(time.Second))
	if err != nil {
		t.Fatalf("recover served snapshot: %v", err)
	}
	if refreshedAt != now.Add(time.Second) || stub.readyCalls != 1 || stub.activations != 1 {
		t.Fatalf("unexpected recovery result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}

func TestMaintainServedSnapshotKeepsFailureClosed(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	stub := &snapshotMaintenanceStub{
		readyErr:      errors.New("receipt expired"),
		activationErr: errors.New("readback unavailable"),
	}

	refreshedAt, err := maintainServedSnapshot(context.Background(), stub, now, now.Add(time.Second))
	if err == nil {
		t.Fatal("unavailable readback was accepted")
	}
	if refreshedAt != now || stub.readyCalls != 1 || stub.activations != 1 {
		t.Fatalf("unexpected failed recovery result: refreshed=%s ready_calls=%d activations=%d", refreshedAt, stub.readyCalls, stub.activations)
	}
}
