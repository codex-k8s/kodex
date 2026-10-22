package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
)

// controlledFreshnessStore embeds no-op persistence and supplies an exact freshness sample, optional error and read callback. It models timing and failure without a database or durable lease.
// controlledFreshnessStore встраивает фиктивное хранилище и задаёт точный образец свежести, необязательную ошибку и callback чтения. Время и отказ моделируются без БД и устойчивой аренды.
type controlledFreshnessStore struct {
	testAuthorityStore
	sample  repository.SnapshotFreshness
	failure error
	onRead  func()
}

// Freshness invokes the optional read callback before returning the configured sample and error unchanged, allowing a unit to advance local time during a simulated round trip.
// Freshness вызывает необязательный callback чтения до возврата настроенных образца и ошибки без изменения, позволяя unit продвинуть локальные часы за моделируемый сетевой обмен.
func (s *controlledFreshnessStore) Freshness(context.Context, repository.SnapshotState) (repository.SnapshotFreshness, error) {
	if s.onRead != nil {
		s.onRead()
	}
	return s.sample, s.failure
}

// newFreshnessTestAuthority builds a fresh controlled store and Authority sharing
// the caller's nonnil mutable local clock, with a fixed thirty-second sample
// from the supplied database time. It does not activate a receipt or persist it.
// newFreshnessTestAuthority создаёт новые управляемые store и Authority с общими
// переданными ненулевыми изменяемыми локальными часами и тридцатисекундным образцом
// от времени БД. Квитанция не активируется и не сохраняется.
func newFreshnessTestAuthority(db time.Time, local *time.Time, receiptID string) (*Authority, *controlledFreshnessStore) {
	store := &controlledFreshnessStore{sample: repository.SnapshotFreshness{ReceiptID: receiptID, ObservedAt: db, ValidUntil: db.Add(30 * time.Second)}}
	authority := &Authority{store: store, now: func() time.Time { return *local }}
	return authority, store
}

// TestDurableFreshnessClockAndOutageBoundaries varies local clock offset around a fixed database sample and checks that repeating activation or recreating Authority cannot renew an expired receipt. No actual attestor outage, database or restart runs.
// TestDurableFreshnessClockAndOutageBoundaries меняет смещение локальных часов относительно фиксированного образца БД и проверяет, что повторная активация и новый Authority не обновляют истёкшую квитанцию. Реальный отказ attestor, БД и перезапуск не выполняются.
func TestDurableFreshnessClockAndOutageBoundaries(t *testing.T) {
	// receipt fixes the synthetic durable lease identity across offset and restart cases.
	// receipt фиксирует синтетическую идентичность устойчивой аренды между случаями смещения и перезапуска.
	const receipt = "13130000-0000-4000-8000-000000000100"
	for _, offset := range []time.Duration{-5 * time.Second, 0, 5 * time.Second} {
		t.Run(offset.String(), func(t *testing.T) {
			db := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
			local := db.Add(offset)
			authority, store := newFreshnessTestAuthority(db, &local, receipt)
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

// TestDurableFreshnessConsumesNetworkTimeAndFailsOnDatabaseLoss advances a fake local clock during the freshness callback and injects a store error, asserting both paths reject instead of extending the budget or using an in-memory fallback.
// TestDurableFreshnessConsumesNetworkTimeAndFailsOnDatabaseLoss продвигает фиктивные локальные часы внутри callback свежести и внедряет ошибку store, требуя отказа вместо продления бюджета и запасного пути в памяти.
func TestDurableFreshnessConsumesNetworkTimeAndFailsOnDatabaseLoss(t *testing.T) {
	db := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	local := db
	authority, store := newFreshnessTestAuthority(db, &local, "13130000-0000-4000-8000-000000000100")
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

// TestDurableFreshnessRejectsCorruptionAndSlidingSameReceipt rejects a renewed validity interval for the same local receipt, an invalid receipt ID and a fresh ID with an overlong database budget using controlled samples.
// TestDurableFreshnessRejectsCorruptionAndSlidingSameReceipt на управляемых образцах отклоняет продлённый срок прежней локальной квитанции, неверный ID и новый ID с чрезмерным бюджетом БД.
func TestDurableFreshnessRejectsCorruptionAndSlidingSameReceipt(t *testing.T) {
	db := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	local := db
	authority, store := newFreshnessTestAuthority(db, &local, "13130000-0000-4000-8000-000000000100")
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

// TestFreshnessAdoptsExactReplicaReceiptForWorkingAccept changes the store's receipt after local activation and checks Ready adopts that exact receipt in SnapshotState. It does not execute a database acceptance transaction or cross-replica transport.
// TestFreshnessAdoptsExactReplicaReceiptForWorkingAccept меняет квитанцию store после локальной активации и проверяет принятие Ready именно этой квитанции в SnapshotState. Транзакция принятия в БД и межрепличный транспорт не выполняются.
func TestFreshnessAdoptsExactReplicaReceiptForWorkingAccept(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	// first pins the locally activated fixture receipt before the authoritative sample changes.
	// first закрепляет локально активированную квитанцию эталона до изменения авторитетного образца.
	const first = "13130000-0000-4000-8000-000000000100"
	// second identifies the distinct authoritative sample adopted by readiness without a new local activation.
	// second обозначает отдельный авторитетный образец, принимаемый readiness без новой локальной активации.
	const second = "13130000-0000-4000-8000-000000000200"
	authority, store := newFreshnessTestAuthority(now, &now, first)
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
