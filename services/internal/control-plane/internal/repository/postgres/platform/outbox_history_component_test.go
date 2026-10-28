package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// outboxHistorySeed creates published history and distinct pending predecessor states in an isolated database.
// outboxHistorySeed создаёт опубликованную историю и разные состояния предшественников в изолированной БД.
//
//go:embed testdata/sql/outbox_history_seed.sql
var outboxHistorySeed string

// outboxHistoryLock blocks the disposable table to verify the repository's own claim deadline.
// outboxHistoryLock блокирует тестовую таблицу для проверки собственного срока claim в repository.
//
//go:embed testdata/sql/outbox_history_lock.sql
var outboxHistoryLock string

// TestOutboxPublishedHistoryComponent requires bounded real claims after a long published history while
// preserving unpublished predecessors, exclusive leases, and publication-driven advancement.
// TestOutboxPublishedHistoryComponent требует ограниченного реального claim после большой опубликованной
// истории, сохраняя неопубликованных предшественников, исключительность lease и продвижение после publish.
func TestOutboxPublishedHistoryComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated outbox database")
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, outboxHistorySeed); err != nil {
		t.Fatal("seed outbox history")
	}
	repository := &Repository{pool: pool}
	claimContext, cancelClaim := context.WithTimeout(ctx, 5*time.Second)
	started := time.Now()
	items, err := repository.ClaimOutbox(claimContext, "history-worker", 128, time.Minute)
	cancelClaim()
	if err != nil {
		t.Fatalf("claim after published history failed within bounded budget: %v", err)
	}
	t.Logf("claim after 70000 published and 8000 pending events: %s", time.Since(started))
	if len(items) != 2 {
		t.Fatalf("expected one history head and one independent event, got %d", len(items))
	}
	var history OutboxItem
	for _, item := range items {
		switch string(item.Payload) {
		case "70001":
			history = item
		case "independent":
		default:
			t.Fatal("claim bypassed an unpublished predecessor")
		}
	}
	if history.EventID == "" || history.Attempts != 1 {
		t.Fatal("history head was not claimed exactly once")
	}
	competing, err := repository.ClaimOutbox(ctx, "other-worker", 128, time.Minute)
	if err != nil || len(competing) != 0 {
		t.Fatal("competing worker bypassed a claimed predecessor")
	}
	forged := history
	forged.LeaseToken = "other-worker:foreign-lease"
	receipt := eventing.PublishReceipt{Stream: "fixture", Sequence: 1}
	if err := repository.MarkOutboxPublished(ctx, forged, receipt); err == nil {
		t.Fatal("foreign lease published an event")
	}
	if err := repository.MarkOutboxPublished(ctx, history, receipt); err != nil {
		t.Fatal("publish exact history head")
	}
	next, err := repository.ClaimOutbox(ctx, "history-worker", 128, time.Minute)
	if err != nil || len(next) != 1 || string(next[0].Payload) != "70002" {
		t.Fatal("publication did not advance exactly one ordered successor")
	}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin outbox lock fixture")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = lock.Rollback(cleanup)
	}()
	if _, err := lock.Exec(ctx, outboxHistoryLock); err != nil {
		t.Fatal("lock disposable outbox")
	}
	started = time.Now()
	blocked, err := repository.ClaimOutbox(ctx, "blocked-worker", 128, time.Minute)
	if !errors.Is(err, errs.ErrUnavailable) || len(blocked) != 0 || time.Since(started) > 10*time.Second || ctx.Err() != nil {
		t.Fatal("claim did not stop within its own deadline without a partial batch")
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal("release disposable outbox lock")
	}
	if _, err := repository.ClaimOutbox(ctx, "recovered-worker", 128, time.Minute); err != nil {
		t.Fatal("claim did not recover after bounded cancellation")
	}
}
