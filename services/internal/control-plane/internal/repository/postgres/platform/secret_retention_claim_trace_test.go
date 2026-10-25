package platform

import (
	"context"
	"sync"
	"testing"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// secretRetentionTraceKey marks only the canonical queue-selection statement in the synthetic worker context.
// secretRetentionTraceKey отмечает лишь канонический queue-selection statement в контексте синтетического worker.
type secretRetentionTraceKey struct{}

// secretRetentionClaimTrace pauses the first completed queue read using channels, with no production hook or SQL/state mutation.
// secretRetentionClaimTrace приостанавливает первое завершённое чтение очереди через channels без production hook или SQL/state mutation.
type secretRetentionClaimTrace struct {
	selected, resume chan struct{}
	once             sync.Once
}

// TraceQueryStart tags the exact candidate query without recording its arguments or any credential material.
// TraceQueryStart отмечает exact candidate query без записи его аргументов либо credential material.
func (trace *secretRetentionClaimTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryRuntimeClaimExecutionSelectClaimableAgentExecutions {
		return context.WithValue(ctx, secretRetentionTraceKey{}, trace)
	}
	return ctx
}

// TraceQueryEnd signals that Rows.Close released the result stream, then waits for owner commands or context cancellation before the claim continues.
// TraceQueryEnd сообщает, что Rows.Close освободил result stream, затем ждёт owner commands либо отмену контекста до продолжения claim.
func (trace *secretRetentionClaimTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil || ctx.Value(secretRetentionTraceKey{}) != trace {
		return
	}
	trace.once.Do(func() {
		close(trace.selected)
		select {
		case <-trace.resume:
		case <-ctx.Done():
		}
	})
}

// secretRetentionClaimResult carries only the native command result/error from the paused synthetic worker.
// secretRetentionClaimResult переносит лишь native command result/error приостановленного синтетического worker.
type secretRetentionClaimResult struct {
	result command.Result
	err    error
}

// pauseSecretRetentionClaim starts one traced native ClaimExecution against the same disposable owner database and joins/cancels it during cleanup; no model, Pod or external provider starts.
// pauseSecretRetentionClaim запускает один traced native ClaimExecution в той же disposable owner database и завершает/отменяет его при cleanup; модель, Pod и внешний провайдер не запускаются.
func pauseSecretRetentionClaim(t *testing.T, ctx context.Context, repository *Repository, worker value.Principal) (func(), <-chan secretRetentionClaimResult) {
	t.Helper()
	claimContext, cancel := context.WithCancel(ctx)
	trace := &secretRetentionClaimTrace{selected: make(chan struct{}), resume: make(chan struct{})}
	config := repository.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(claimContext, config)
	if err != nil {
		cancel()
		t.Fatal("construct paused disposable worker pool")
	}
	copy := *repository
	copy.pool = pool
	service, err := platformservice.New(&copy)
	if err != nil {
		cancel()
		pool.Close()
		t.Fatal(err)
	}
	done := make(chan secretRetentionClaimResult, 1)
	exited := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(trace.resume) }) }
	t.Cleanup(func() {
		release()
		cancel()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-exited:
			pool.Close()
		case <-timer.C:
			t.Error("paused claim did not join within cleanup budget")
		}
	})
	go func() {
		defer close(exited)
		result, err := service.Execute(claimContext, command.Command{Kind: command.ClaimExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: "secret-retention-stale-candidate-claim"}, Payload: command.LeaseInput{WorkloadInstance: "secret-retention-stale-worker", Limit: 10}})
		done <- secretRetentionClaimResult{result: result, err: err}
	}()
	select {
	case <-trace.selected:
		return release, done
	case result := <-done:
		t.Fatalf("claim completed before candidate barrier: %v", result.err)
	case <-ctx.Done():
		t.Fatal("candidate barrier exceeded fixture budget")
	}
	return release, done
}
