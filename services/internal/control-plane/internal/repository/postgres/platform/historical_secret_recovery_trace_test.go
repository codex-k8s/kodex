package platform

import (
	"context"
	_ "embed"
	"strings"
	"sync"
	"testing"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// historicalSecretRecoveryWait reads only backend lock metadata, never SQL text, parameters, credentials or secret values.
// historicalSecretRecoveryWait читает лишь backend lock metadata, без SQL text, parameters, credentials и значений секретов.
//
//go:embed testdata/sql/historical_secret_recovery_wait.sql
var historicalSecretRecoveryWait string

// historicalSecretRecoverySnapshot observes aggregate/operation versions and owner-effect counts for rollback/replay checks without changing business state.
// historicalSecretRecoverySnapshot наблюдает aggregate/operation versions и owner-effect counts для rollback/replay checks без изменения business state.
//
//go:embed testdata/sql/historical_secret_recovery_snapshot.sql
var historicalSecretRecoverySnapshot string

// historicalSecretDescriptorFault preserves the descriptor query's exact argument names while forcing a backend read failure.
// historicalSecretDescriptorFault сохраняет exact argument names descriptor query, вызывая backend read failure.
//
//go:embed testdata/sql/historical_secret_recovery_descriptor_fault.sql
var historicalSecretDescriptorFault string

// historicalSecretRetentionFault preserves all retention pins while forcing a backend failure before the decision.
// historicalSecretRetentionFault сохраняет все retention pins, вызывая backend failure до принятия решения.
//
//go:embed testdata/sql/historical_secret_recovery_retention_fault.sql
var historicalSecretRetentionFault string

// historicalSecretRetireFault rejects the exact retirement statement without creating a test-only business mutation.
// historicalSecretRetireFault отклоняет exact retirement statement без создания test-only business mutation.
//
//go:embed testdata/sql/historical_secret_recovery_retire_fault.sql
var historicalSecretRetireFault string

// historicalSecretIntentFault fails cleanup-intent persistence after the real retirement statement to prove that both roll back together.
// historicalSecretIntentFault отклоняет cleanup-intent persistence после настоящего retirement statement, доказывая их совместный rollback.
//
//go:embed testdata/sql/historical_secret_recovery_intent_fault.sql
var historicalSecretIntentFault string

// historicalSecretTraceKey tags only the selected command phase on a disposable connection.
// historicalSecretTraceKey отмечает лишь выбранную command phase в disposable connection.
type historicalSecretTraceKey struct{}

// historicalSecretTraceMark carries a private tracer identity and nonsensitive backend PID between pgx callbacks.
// historicalSecretTraceMark переносит private tracer identity и несекретный backend PID между pgx callbacks.
type historicalSecretTraceMark struct {
	trace *historicalSecretTrace
	pid   uint32
}

// historicalSecretTrace either observes a query start PID or pauses its completed response; observing start alone never claims a server snapshot exists.
// historicalSecretTrace либо наблюдает query start PID, либо приостанавливает завершённый ответ; сам start не считается доказательством server snapshot.
type historicalSecretTrace struct {
	query       string
	observeOnly bool
	selected    chan uint32
	resume      chan struct{}
	once        sync.Once
}

// TraceQueryStart captures only the exact phase and backend PID, without recording query arguments or model data.
// TraceQueryStart фиксирует лишь exact phase и backend PID, без query arguments и model data.
func (trace *historicalSecretTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	matched := strings.Contains(data.SQL, "-- name: "+trace.query+" ") || trace.query == "rollback" && strings.EqualFold(strings.TrimSpace(data.SQL), "rollback")
	if !matched {
		return ctx
	}
	pid := conn.PgConn().PID()
	if trace.observeOnly {
		trace.once.Do(func() { trace.selected <- pid })
		return ctx
	}
	return context.WithValue(ctx, historicalSecretTraceKey{}, historicalSecretTraceMark{trace: trace, pid: pid})
}

// TraceQueryEnd pauses only after successful query completion, so publisher SHARE locks or a completed ROLLBACK are already authoritative before the other operation runs.
// TraceQueryEnd приостанавливает лишь успешное завершение query, поэтому publisher SHARE locks либо завершённый ROLLBACK уже авторитетны до запуска другой operation.
func (trace *historicalSecretTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	mark, ok := ctx.Value(historicalSecretTraceKey{}).(historicalSecretTraceMark)
	if !ok || mark.trace != trace || data.Err != nil {
		return
	}
	trace.once.Do(func() {
		trace.selected <- mark.pid
		select {
		case <-trace.resume:
		case <-ctx.Done():
		}
	})
}

// historicalSecretOutcome exposes only a safe action/error result from one joined native owner call.
// historicalSecretOutcome раскрывает лишь safe action/error result одного joined native owner call.
type historicalSecretOutcome struct {
	action string
	err    error
}

// startHistoricalSecretCall creates a traced adapter for the same disposable database and cancels/joins it before closing its pool; no production hook or external service is involved.
// startHistoricalSecretCall создаёт traced adapter для той же disposable database и отменяет/завершает его до закрытия pool; production hook и внешние сервисы не используются.
func startHistoricalSecretCall(t *testing.T, parent context.Context, r *Repository, phase string, observe bool, call func(context.Context, *platformservice.Service) (string, error)) (*historicalSecretTrace, func(), <-chan historicalSecretOutcome) {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	trace := &historicalSecretTrace{query: phase, observeOnly: observe, selected: make(chan uint32, 1), resume: make(chan struct{})}
	config := r.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		cancel()
		t.Fatal("construct traced historical recovery pool")
	}
	copy := *r
	copy.pool = pool
	s, err := platformservice.New(&copy)
	if err != nil {
		cancel()
		pool.Close()
		t.Fatal(err)
	}
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
			t.Error("historical recovery call failed bounded join")
		}
	})
	go func() {
		defer close(exited)
		action, err := call(ctx, s)
		done <- historicalSecretOutcome{action: action, err: err}
	}()
	return trace, release, done
}

// awaitHistoricalSecretPhase waits for the exact callback or a bounded native-call failure, without guessed timing sleeps.
// awaitHistoricalSecretPhase ждёт exact callback либо bounded native-call failure без угадывающих timing sleeps.
func awaitHistoricalSecretPhase(t *testing.T, ctx context.Context, trace *historicalSecretTrace, done <-chan historicalSecretOutcome) uint32 {
	t.Helper()
	select {
	case pid := <-trace.selected:
		return pid
	case result := <-done:
		t.Fatalf("native call ended before historical phase: %v", result.err)
	case <-ctx.Done():
		t.Fatal("historical phase exceeded fixture budget")
	}
	return 0
}

// awaitHistoricalSecretResult joins one expected native result using the shared bounded fixture context.
// awaitHistoricalSecretResult завершает один ожидаемый native result в общем bounded fixture context.
func awaitHistoricalSecretResult(t *testing.T, ctx context.Context, done <-chan historicalSecretOutcome) historicalSecretOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-ctx.Done():
		t.Fatal("historical owner call exceeded fixture budget")
	}
	return historicalSecretOutcome{}
}

// awaitHistoricalSecretLock proves an actual server Lock wait on the exact publisher PID; pgx TraceQueryStart alone is explicitly insufficient.
// awaitHistoricalSecretLock доказывает настоящий server Lock wait на exact publisher PID; одного pgx TraceQueryStart явно недостаточно.
func awaitHistoricalSecretLock(t *testing.T, parent context.Context, pool *pgxpool.Pool, waiter, blocker uint32, done <-chan historicalSecretOutcome) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, historicalSecretRecoveryWait, int64(waiter), int64(blocker)).Scan(&waiting); err != nil {
			t.Fatal("read actual recovery lock wait metadata")
		}
		if waiting {
			return
		}
		select {
		case result := <-done:
			t.Fatalf("recovery returned before actual lock wait: %v", result.err)
		case <-ctx.Done():
			t.Fatal("recovery did not enter exact server lock wait")
		case <-ticker.C:
		}
	}
}

// snapshot reads only the synthetic Secret/operation state and effect counts, never the materialized value.
// snapshot читает лишь синтетическое Secret/operation state и effect counts, без materialized value.
func (f *historicalRecoveryFixture) snapshot(t *testing.T, ctx context.Context) string {
	t.Helper()
	var snapshot string
	if err := f.repository.pool.QueryRow(ctx, historicalSecretRecoverySnapshot, f.secret.Ref, f.operationRef).Scan(&snapshot); err != nil {
		t.Fatal("read historical recovery owner snapshot")
	}
	return snapshot
}
