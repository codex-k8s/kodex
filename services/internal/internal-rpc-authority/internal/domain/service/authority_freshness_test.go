package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
)

type controlledFreshnessStore struct {
	testAuthorityStore
	sample  repository.SnapshotFreshness
	failure error
	onRead  func()
}

func (s *controlledFreshnessStore) Freshness(context.Context, repository.SnapshotState) (repository.SnapshotFreshness, error) {
	if s.onRead != nil {
		s.onRead()
	}
	return s.sample, s.failure
}

func TestDurableFreshnessClockAndOutageBoundaries(t *testing.T) {
	const receipt = "13130000-0000-4000-8000-000000000100"
	for _, offset := range []time.Duration{-5 * time.Second, 0, 5 * time.Second} {
		t.Run(offset.String(), func(t *testing.T) {
			db := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
			local := db.Add(offset)
			store := &controlledFreshnessStore{sample: repository.SnapshotFreshness{ReceiptID: receipt, ObservedAt: db, ValidUntil: db.Add(30 * time.Second)}}
			authority := &Authority{store: store, now: func() time.Time { return local }}
			if err := authority.ActivateSnapshot(t.Context(), receipt); err != nil {
				t.Fatal("activation rejected")
			}
			local = local.Add(29 * time.Second)
			store.sample.ObservedAt = db.Add(29 * time.Second)
			if err := authority.Ready(t.Context()); err != nil {
				t.Fatal("unexpired receipt rejected during attestor outage")
			}
			// Переотправка старого receipt и повторный readback не дают нового бюджета.
			if err := authority.ActivateSnapshot(t.Context(), receipt); err != nil {
				t.Fatal("exact unexpired receipt rejected")
			}
			local = local.Add(time.Second)
			store.sample.ObservedAt = db.Add(30 * time.Second)
			if err := authority.Ready(t.Context()); err == nil {
				t.Fatal("clock skew extended durable deadline")
			}
			restarted := &Authority{store: store, now: func() time.Time { return local }}
			if err := restarted.ActivateSnapshot(t.Context(), receipt); err == nil {
				t.Fatal("restart renewed expired receipt")
			}
		})
	}
}

func TestDurableFreshnessConsumesNetworkTimeAndFailsOnDatabaseLoss(t *testing.T) {
	db := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	local := db
	store := &controlledFreshnessStore{sample: repository.SnapshotFreshness{ReceiptID: "13130000-0000-4000-8000-000000000100", ObservedAt: db, ValidUntil: db.Add(30 * time.Second)}}
	authority := &Authority{store: store, now: func() time.Time { return local }}
	if err := authority.ActivateSnapshot(t.Context(), store.sample.ReceiptID); err != nil {
		t.Fatal("activation rejected")
	}
	store.sample.ObservedAt = db.Add(29 * time.Second)
	local = db.Add(29 * time.Second)
	store.onRead = func() { local = local.Add(2 * time.Second) }
	if err := authority.Ready(t.Context()); err == nil {
		t.Fatal("network round trip extended deadline")
	}
	store.onRead = nil
	store.failure = errors.New("database unavailable")
	local = db.Add(time.Second)
	if err := authority.Ready(t.Context()); err == nil {
		t.Fatal("database outage used in-memory replay fallback")
	}
}

func TestDurableFreshnessRejectsCorruptionAndSlidingSameReceipt(t *testing.T) {
	db := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	local := db
	store := &controlledFreshnessStore{sample: repository.SnapshotFreshness{ReceiptID: "13130000-0000-4000-8000-000000000100", ObservedAt: db, ValidUntil: db.Add(30 * time.Second)}}
	authority := &Authority{store: store, now: func() time.Time { return local }}
	if err := authority.ActivateSnapshot(t.Context(), store.sample.ReceiptID); err != nil {
		t.Fatal("activation rejected")
	}
	local = db.Add(31 * time.Second)
	store.sample.ObservedAt = local
	store.sample.ValidUntil = local.Add(30 * time.Second)
	if err := authority.Ready(t.Context()); err == nil {
		t.Fatal("same receipt readback renewed local lease")
	}
	store.sample.ReceiptID = "invalid"
	if err := authority.Ready(t.Context()); err == nil {
		t.Fatal("invalid receipt identity accepted")
	}
	store.sample.ReceiptID = "13130000-0000-4000-8000-000000000200"
	store.sample.ValidUntil = local.Add(31 * time.Second)
	if err := authority.Ready(t.Context()); err == nil {
		t.Fatal("overlong database budget accepted")
	}
}

func TestFreshnessAdoptsExactReplicaReceiptForWorkingAccept(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	const first = "13130000-0000-4000-8000-000000000100"
	const second = "13130000-0000-4000-8000-000000000200"
	store := &controlledFreshnessStore{sample: repository.SnapshotFreshness{ReceiptID: first, ObservedAt: now, ValidUntil: now.Add(30 * time.Second)}}
	authority := &Authority{store: store, now: func() time.Time { return now }}
	if err := authority.ActivateSnapshot(t.Context(), first); err != nil {
		t.Fatal("activation failed")
	}
	store.sample.ReceiptID = second
	if err := authority.Ready(t.Context()); err != nil {
		t.Fatal("replica receipt readiness rejected")
	}
	if authority.SnapshotState().AttestationReceiptID != second {
		t.Fatal("working accept retained a different receipt than authoritative readiness")
	}
}
