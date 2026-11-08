package platform

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// authorityCounterTraceKey identifies the completed owner write inside the driver's query context,
// without retaining arguments or changing the caller's authority or transaction context.
// authorityCounterTraceKey отмечает завершённую owner write в query context драйвера без
// сохранения аргументов либо изменения полномочий и transaction context вызывающего.
type authorityCounterTraceKey struct{}

// authorityCounterInterleave allocates a real proof revision on an independent connection after
// each successful owner write, while the SERIALIZABLE owner transaction remains open. It does not
// inject SQL errors, alter isolation or retry the caller's external operation.
// authorityCounterInterleave выделяет настоящий proof revision в независимом соединении после
// успешной owner write, пока SERIALIZABLE owner transaction открыта. Не внедряет SQL errors,
// не меняет isolation и не повторяет внешнюю операцию вызывающего.
type authorityCounterInterleave struct {
	issuer    *Repository
	marker    string
	allocates atomic.Int64
	failed    atomic.Bool
}

// TraceQueryStart marks only the exact query that writes the tested owner aggregate; the trace
// neither reads raw payloads nor treats query start as proof of a completed database snapshot.
// TraceQueryStart отмечает только точный query записи проверяемого owner aggregate; trace не
// читает raw payload и не считает начало query доказательством завершённого DB snapshot.
func (trace *authorityCounterInterleave) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, authorityCounterTraceKey{}, strings.Contains(data.SQL, trace.marker))
}

// TraceQueryEnd runs the real autocommit allocator only after a successful native owner write;
// the outer transaction's snapshot and partial writes already exist at this deterministic barrier.
// TraceQueryEnd запускает настоящий autocommit allocator лишь после успешной native owner write;
// snapshot и partial writes внешней transaction уже существуют на этом deterministic barrier.
func (trace *authorityCounterInterleave) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	matched, _ := ctx.Value(authorityCounterTraceKey{}).(bool)
	if !matched || data.Err != nil || data.CommandTag.RowsAffected() != 1 {
		return
	}
	trace.allocates.Add(1)
	if revision, err := trace.issuer.NextAuthorityProofRevision(ctx); err != nil || revision == 0 {
		trace.failed.Store(true)
	}
}

// TestAuthorityProofCounterOwnerConcurrencyComponent interleaves real independent proof commits
// after each owner write. Both recipe scopes and both admission verdicts must commit once despite
// continuous proof issuance, preserving receipt/report/event cardinality and the existing retry cap.
// TestAuthorityProofCounterOwnerConcurrencyComponent вставляет настоящие независимые proof commits
// после каждой owner write. Обе области recipe и оба admission verdict должны commit один раз
// при выдаче proofs, сохраняя cardinality receipt/report/event и прежний предел retry.
func TestAuthorityProofCounterOwnerConcurrencyComponent(t *testing.T) {
	for _, scopeKind := range []string{"PROJECT", "ORGANIZATION"} {
		for _, operation := range []string{"manage", "record-ACCEPTED", "record-REJECTED"} {
			t.Run(scopeKind+"/"+operation, func(t *testing.T) {
				ctx, pool, owner, manage, record := roleImageOwnerRetryFixture(t, scopeKind, strings.TrimPrefix(operation, "record-"))
				trace := &authorityCounterInterleave{issuer: &Repository{pool: pool}, marker: "-- name: role_images_record_admission :one"}
				if operation == "manage" {
					trace.marker = "-- name: role_images_insert_build :one"
				}
				config := pool.Config()
				config.ConnConfig.Tracer = trace
				traced, err := pgxpool.NewWithConfig(ctx, config)
				if err != nil {
					t.Fatal("open isolated traced owner connection")
				}
				defer traced.Close()
				owner.pool = traced
				before := roleImageOwnerRetrySnapshot(t, ctx, pool)
				_, err = roleImageOwnerRetryInvoke(ctx, owner, strings.Split(operation, "-")[0], manage, record)
				if trace.failed.Load() || err != nil {
					t.Fatalf("proof allocation prevented owner commit: allocations=%d ownerErrorClass=%T", trace.allocates.Load(), err)
				}
				after := roleImageOwnerRetrySnapshot(t, ctx, pool)
				buildDelta, reportDelta := int64(0), int64(1)
				if operation == "manage" {
					buildDelta, reportDelta = 1, 0
				}
				if trace.allocates.Load() != 1 || after.builds != before.builds+buildDelta || after.reports != before.reports+reportDelta ||
					after.receipts != before.receipts+1 || after.events != before.events+1 || after.sequence != before.sequence+1 {
					t.Fatal("independent allocator left duplicate or incomplete owner effects")
				}
			})
		}
	}
}
