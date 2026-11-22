package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesInputClaimDeadlineSQL observes only the already committed attempt deadline, not payload or credentials.
// kubernetesInputClaimDeadlineSQL читает только committed attempt deadline, не payload либо credentials.
//
//go:embed testdata/sql/kubernetes_input_claim_deadline.sql
var kubernetesInputClaimDeadlineSQL string

// kubernetesInputCommitMarker identifies native COMMIT without retaining query arguments.
// kubernetesInputCommitMarker отмечает native COMMIT без хранения query arguments.
type kubernetesInputCommitMarker struct{}

// kubernetesInputCommitTrace arms only after the real payload body was read and closed.
// It delays the second owner transaction's successful COMMIT acknowledgement past its original deadline.
// kubernetesInputCommitTrace активируется лишь после actual payload read и close.
// Задерживает successful COMMIT acknowledgement второй owner transaction до истечения original deadline.
type kubernetesInputCommitTrace struct {
	ready, armed atomic.Bool
	until        atomic.Int64
	hits         atomic.Int32
}

// TraceQueryStart targets the post-read owner snapshot, excluding the durable first Claim.
// TraceQueryStart выбирает post-read owner snapshot, исключая durable first Claim.
func (tr *kubernetesInputCommitTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if tr.ready.Load() && d.SQL == queryKubernetesInputStage {
		tr.armed.Store(true)
	}
	return context.WithValue(ctx, kubernetesInputCommitMarker{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}

// TraceQueryEnd delays a real successful COMMIT; it never fabricates a driver error or changes database time.
// TraceQueryEnd задерживает real successful COMMIT, не подделывает driver error и не меняет database time.
func (tr *kubernetesInputCommitTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(kubernetesInputCommitMarker{}).(bool)
	if !commit || d.Err != nil || !tr.armed.CompareAndSwap(true, false) {
		return
	}
	tr.hits.Add(1)
	timer := time.NewTimer(time.Until(time.Unix(0, tr.until.Load()).Add(25 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// kubernetesInputDisclosureStore counts the single body read and arms the fault only after its positive close.
// kubernetesInputDisclosureStore учитывает единственное чтение body и активирует fault лишь после positive close.
type kubernetesInputDisclosureStore struct {
	objectstorage.Store
	afterClose func() error
	gets       atomic.Int32
}

// Get retains the actual immutable fixture object and wraps only its close boundary.
// Get сохраняет actual immutable fixture object и оборачивает лишь close boundary.
func (s *kubernetesInputDisclosureStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	s.gets.Add(1)
	object, err := s.Store.Get(ctx, key, version)
	if err == nil && object.Body != nil {
		object.Body = &kubernetesInputDisclosureBody{ReadCloser: object.Body, afterClose: s.afterClose}
	}
	return object, err
}

// kubernetesInputDisclosureBody preserves the original bytes while observing completed body ownership.
// kubernetesInputDisclosureBody сохраняет original bytes, наблюдая завершённое body ownership.
type kubernetesInputDisclosureBody struct {
	io.ReadCloser
	afterClose func() error
}

// Close releases the actual provider body before enabling the post-read commit fault.
// Close освобождает actual provider body до активации post-read commit fault.
func (b *kubernetesInputDisclosureBody) Close() error {
	if err := b.ReadCloser.Close(); err != nil {
		return err
	}
	return b.afterClose()
}

// TestKubernetesInputDisclosureDeadlineOwnerComponent requires a real delayed successful COMMIT to withhold bytes and preserve one attempt.
// It uses existing claimed/approved owner graph and fixture storage; target/native execution and FILE ACL pinning are not proved here.
// TestKubernetesInputDisclosureDeadlineOwnerComponent требует не выдавать bytes после real delayed successful COMMIT и сохранять одну attempt.
// Использует existing claimed/approved owner graph и fixture storage; target/native execution и FILE ACL pinning не доказывает.
func TestKubernetesInputDisclosureDeadlineOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	r, service, _, gateway, binding, _, admission, _, actor := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
	stream := admission.Stream.Ref
	actor.Permission = "platform.integrations.kubernetes.input.get"
	read, err := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("synthetic one-attempt input")
	defer clear(body)
	sum := sha256.Sum256(body)
	actor.Permission = "platform.integrations.kubernetes.input.submit"
	queued, err := service.SubmitKubernetesStreamInput(ctx, actor, value.Mutation{IdempotencyKey: "input-disclosure-once", ExpectedVersion: &read.View.Version}, command.KubernetesStreamInputSubmit{InvocationRef: binding.InvocationRef, StreamRef: stream, Channel: "STDIN", PayloadDigest: hex.EncodeToString(sum[:]), Payload: body})
	if err != nil || queued.Submission == nil {
		t.Fatal("queue actual input source", err)
	}
	trace := &kubernetesInputCommitTrace{}
	config := r.pool.Config().Copy()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	target := *r
	target.pool = pool
	store := &kubernetesInputDisclosureStore{Store: r.objects}
	target.objects = store
	store.afterClose = func() error {
		var until time.Time
		if err := r.pool.QueryRow(ctx, kubernetesInputClaimDeadlineSQL, queued.Submission.Ref, stream).Scan(&until); err != nil {
			return err
		}
		trace.until.Store(until.UnixNano())
		trace.ready.Store(true)
		return nil
	}
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInputFrame_FullMethodName {
			gateway.Permission = operation
		}
	}
	if gateway.Permission != "platform.runtime.kubernetes.input.claim" {
		t.Fatal("production claim operation is missing")
	}
	resolvedGateway, err := r.ResolvePrincipal(ctx, gateway)
	if err != nil {
		t.Fatal("resolve input claim gateway", err)
	}
	result, err := target.ClaimManagedKubernetesInputFrame(ctx, resolvedGateway, binding, stream)
	if trace.hits.Load() != 1 {
		if result.Frame != nil {
			clear(result.Frame.Payload)
		}
		t.Fatal("actual post-read COMMIT fault was not reached", err)
	}
	if err == nil || result.Frame != nil {
		if result.Frame != nil {
			clear(result.Frame.Payload)
		}
		t.Fatal("expired claim returned private payload", err)
	}
	trace.ready.Store(false)
	again, err := target.ClaimManagedKubernetesInputFrame(ctx, resolvedGateway, binding, stream)
	if err != nil || again.Disposition != enum.KubernetesInputFrameUnknown || again.Frame != nil || store.gets.Load() != 1 {
		if again.Frame != nil {
			clear(again.Frame.Payload)
		}
		t.Fatal("expired delivery was retried or replayed", err)
	}
	gateway.Permission = "platform.runtime.kubernetes.cancel"
	if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
		t.Fatal("close actual input owner graph", err)
	}
}
