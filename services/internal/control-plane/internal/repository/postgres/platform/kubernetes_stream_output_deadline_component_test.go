package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesOutputACLDeadlineSQL bounds only the disposable fixture's existing authorization bindings.
// kubernetesOutputACLDeadlineSQL ограничивает лишь существующие authorization bindings disposable fixture.
//
//go:embed testdata/sql/kubernetes_output_acl_deadline.sql
var kubernetesOutputACLDeadlineSQL string

// kubernetesDisclosureCommitKey carries only a normalized COMMIT marker, never query arguments.
// kubernetesDisclosureCommitKey переносит только нормализованный COMMIT marker, не query arguments.
type kubernetesDisclosureCommitKey struct{}

// kubernetesDisclosureCommitTrace delays the actual output-read COMMIT acknowledgement past finite ACL expiry.
// kubernetesDisclosureCommitTrace задерживает подтверждение реального output-read COMMIT до истечения finite ACL.
type kubernetesDisclosureCommitTrace struct {
	until time.Time
	armed atomic.Bool
	hits  atomic.Int32
}

// TraceQueryStart arms only the exact output cursor transaction, excluding pool setup and unrelated owner reads.
// TraceQueryStart активирует лишь exact output cursor transaction, исключая pool setup и посторонние owner reads.
func (tr *kubernetesDisclosureCommitTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryKubernetesOutputPublicCursor {
		tr.armed.Store(true)
	}
	return context.WithValue(ctx, kubernetesDisclosureCommitKey{}, strings.EqualFold(strings.TrimSpace(data.SQL), "commit"))
}

// TraceQueryEnd waits after successful native COMMIT without changing rows or the caller deadline.
// TraceQueryEnd ждёт после успешного native COMMIT, не меняя строки либо caller deadline.
func (tr *kubernetesDisclosureCommitTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(kubernetesDisclosureCommitKey{}).(bool)
	if !commit || data.Err != nil || !tr.armed.CompareAndSwap(true, false) {
		return
	}
	tr.hits.Add(1)
	timer := time.NewTimer(time.Until(tr.until.Add(20 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// TestKubernetesStreamOutputDeadlineOwnerComponent proves a real allowed transaction cannot return a live disclosure after its winning ACL expired during COMMIT acknowledgement.
// TestKubernetesStreamOutputDeadlineOwnerComponent доказывает, что реальная allowed transaction не возвращает live disclosure после истечения winning ACL во время подтверждения COMMIT.
func TestKubernetesStreamOutputDeadlineOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
	defer cancel()
	r, _, _, _, binding, _, admission, _, actor := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
	actor.Permission = "platform.integrations.kubernetes.output.read"
	resolvedActor, err := r.ResolvePrincipal(ctx, actor)
	if err != nil {
		t.Fatal("resolve output transport principal", err)
	}
	current, err := r.resolveScope(ctx, resolvedActor)
	if err != nil {
		t.Fatal("resolve output fixture actor", err)
	}
	trace := &kubernetesDisclosureCommitTrace{}
	config := r.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open disclosure trace pool", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("connect disclosure trace pool", err)
	}
	copy := *r
	copy.pool = pool
	trace.until = time.Now().Add(3 * time.Second)
	tag, err := r.pool.Exec(ctx, kubernetesOutputACLDeadlineSQL, current.organizationID, trace.until)
	if err != nil || tag.RowsAffected() == 0 {
		t.Fatal("bound actual fixture ACL", err)
	}
	readStarted := time.Now()
	v, err := copy.ReadKubernetesStreamOutput(ctx, resolvedActor, query.KubernetesStreamOutputRead{InvocationRef: binding.InvocationRef, StreamRef: admission.Stream.Ref, Generation: binding.Generation})
	if trace.hits.Load() != 1 || time.Now().Before(trace.until) || time.Since(readStarted) >= 5*time.Second || ctx.Err() != nil || !errors.Is(err, errs.ErrNotFound) || v.Frame != nil || v.Status != "" {
		t.Fatal("expired ACL escaped after native COMMIT", err)
	}
}
