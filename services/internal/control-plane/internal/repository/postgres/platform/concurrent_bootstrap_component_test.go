package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// concurrentBootstrapReadback reads owner/effect cardinality and exact definition versions,
// never credential values; only native Bootstrap commands modify disposable business state.
// concurrentBootstrapReadback читает owner/effect cardinality и exact definition versions,
// исключая credential values; business state меняют лишь native Bootstrap команды.
//
//go:embed testdata/sql/concurrent_bootstrap_readback.sql
var concurrentBootstrapReadback string

// readConcurrentBootstrapState snapshots immutable owner counts and the full definition version map.
// readConcurrentBootstrapState снимает owner counts и полную карту версий definitions.
func readConcurrentBootstrapState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, map[string]int64) {
	t.Helper()
	var owner, definitions string
	if err := pool.QueryRow(ctx, concurrentBootstrapReadback).Scan(&owner, &definitions); err != nil {
		t.Fatal("read concurrent bootstrap state")
	}
	var versions map[string]int64
	if json.Unmarshal([]byte(definitions), &versions) != nil || len(versions) == 0 {
		t.Fatal("bootstrap definition state invalid")
	}
	return owner, versions
}

// bootstrapProbeTrace composes the existing bounded barrier with private SQLSTATE observations;
// it records no statement text, arguments or driver messages.
// bootstrapProbeTrace объединяет прежний bounded barrier с приватным SQLSTATE observation,
// не записывая statement text, аргументы или driver messages.
type bootstrapProbeTrace struct {
	*historicalSecretTrace
	attempts atomic.Int32
	aborts   atomic.Int32
}

// TraceQueryStart delegates the exact phase barrier and counts whole-transaction starts only.
// TraceQueryStart делегирует exact phase barrier, считая лишь начала целых transactions.
func (trace *bootstrapProbeTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "-- name: repository_bootstrap_select_installation_singleton ") {
		trace.attempts.Add(1)
	}
	return trace.historicalSecretTrace.TraceQueryStart(ctx, conn, data)
}

// TraceQueryEnd records typed serialization/deadlock aborts before invoking the existing barrier.
// TraceQueryEnd отмечает typed serialization/deadlock abort до вызова прежнего barrier.
func (trace *bootstrapProbeTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	var cause *pgconn.PgError
	if errors.As(data.Err, &cause) && (cause.Code == "40001" || cause.Code == "40P01") {
		trace.aborts.Add(1)
	}
	trace.historicalSecretTrace.TraceQueryEnd(ctx, conn, data)
}

// startBootstrapProbe gives each native caller its own traced pool and cancel/join ownership;
// the production repository has no test hooks and no external service is started.
// startBootstrapProbe назначает каждому native caller свой traced pool и cancel/join ownership;
// production repository не имеет test hooks, внешние сервисы не запускаются.
func startBootstrapProbe(t *testing.T, parent context.Context, r *Repository, pause, retry bool) (*bootstrapProbeTrace, func(), context.CancelFunc, <-chan historicalSecretOutcome) {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	trace := &bootstrapProbeTrace{historicalSecretTrace: &historicalSecretTrace{query: "repository_bootstrap_select_installation_singleton", observeOnly: !pause, selected: make(chan uint32, 1), resume: make(chan struct{})}}
	config := r.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		cancel()
		t.Fatal("open traced bootstrap pool")
	}
	copy := *r
	copy.pool = pool
	done := make(chan historicalSecretOutcome, 1)
	exited := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(trace.resume) }) }
	t.Cleanup(func() {
		release()
		cancel()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-exited:
			pool.Close()
		case <-timer.C:
			t.Error("bootstrap native call exceeded bounded join")
		}
	})
	go func() {
		defer close(exited)
		var err error
		if retry {
			err = copy.Bootstrap(ctx)
		} else {
			err = copy.bootstrapOnce(ctx)
		}
		done <- historicalSecretOutcome{err: err}
	}()
	return trace, release, cancel, done
}

// TestConcurrentBootstrapComponent proves the raw SERIALIZABLE abort and its bounded whole-Tx
// recovery with exact server lock metadata, and independently cancels an actual blocked caller.
// TestConcurrentBootstrapComponent доказывает исходный SERIALIZABLE abort и bounded whole-Tx
// recovery через exact server lock metadata, отдельно отменяя действительно заблокированный caller.
func TestConcurrentBootstrapComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	for _, mode := range []string{"single-attempt-abort", "whole-transaction-retry", "cancel-lock-wait"} {
		t.Run(mode, func(t *testing.T) {
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal("open isolated bootstrap database")
			}
			t.Cleanup(pool.Close)
			r := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
			configureComponentArtifactScanner(t, r)
			if err = r.Bootstrap(ctx); err != nil {
				t.Fatal("initialize native bootstrap fixture")
			}
			before, versions := readConcurrentBootstrapState(t, ctx, pool)
			retry := mode != "single-attempt-abort"
			a, release, _, adone := startBootstrapProbe(t, ctx, r, true, retry)
			blocker := awaitHistoricalSecretPhase(t, ctx, a.historicalSecretTrace, adone)
			b, _, cancelB, bdone := startBootstrapProbe(t, ctx, r, false, retry)
			waiter := awaitHistoricalSecretPhase(t, ctx, b.historicalSecretTrace, bdone)
			awaitHistoricalSecretLock(t, ctx, pool, waiter, blocker, bdone)
			var result historicalSecretOutcome
			if mode == "cancel-lock-wait" {
				cancelB()
				result = awaitHistoricalSecretResult(t, ctx, bdone)
				if !errors.Is(result.err, context.Canceled) || b.attempts.Load() != 1 {
					t.Fatal("blocked bootstrap cancellation retried or lost cause")
				}
			}
			release()
			if resultA := awaitHistoricalSecretResult(t, ctx, adone); resultA.err != nil {
				t.Fatal("first native bootstrap failed")
			}
			if mode != "cancel-lock-wait" {
				result = awaitHistoricalSecretResult(t, ctx, bdone)
			}
			increment := int64(1)
			switch mode {
			case "single-attempt-abort":
				var database *pgconn.PgError
				if !errors.As(result.err, &database) || database.Code != "40001" || b.attempts.Load() != 1 || b.aborts.Load() != 1 {
					t.Fatal("raw overlap did not expose exact private40001")
				}
			case "whole-transaction-retry":
				if result.err != nil || b.attempts.Load() != 2 || b.aborts.Load() != 1 {
					t.Fatal("whole bootstrap retry did not rejoin exactly once")
				}
				increment = 2
			}
			after, next := readConcurrentBootstrapState(t, ctx, pool)
			if after != before || len(next) != len(versions) {
				t.Fatal("bootstrap overlap changed immutable owner/effect state")
			}
			for key, version := range versions {
				if next[key] != version+increment {
					t.Fatal("failed bootstrap committed definition writes")
				}
			}
			t.Logf("native bootstrap overlap: actualLock=true attempts=%d retryableAborts=%d committedDefinitionIncrements=%d", b.attempts.Load(), b.aborts.Load(), increment)
		})
	}
}
