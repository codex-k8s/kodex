package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesInputACLRestoreSQL restores only the disposable organization's original active bindings.
// kubernetesInputACLRestoreSQL восстанавливает лишь original active bindings disposable организации.
//
//go:embed testdata/sql/kubernetes_input_acl_restore.sql
var kubernetesInputACLRestoreSQL string

// kubernetesInputPreludeTrace expires the successful first owner read before restoring a current alternative.
// It never edits the stage, fakes a COMMIT result or rewrites the captured first horizon.
// kubernetesInputPreludeTrace завершает successful first owner read после expiry и восстанавливает current alternative.
// Не меняет stage, не подделывает COMMIT result и не переписывает captured first horizon.
type kubernetesInputPreludeTrace struct {
	until        time.Time
	armed, fired atomic.Bool
	hits         atomic.Int32
	restore      func() error
	restoreErr   error
}

// TraceQueryStart targets the actual prelude stage lock, retaining no SQL arguments or payload.
// TraceQueryStart выбирает actual prelude stage lock, не сохраняет SQL arguments либо payload.
func (tr *kubernetesInputPreludeTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if !tr.fired.Load() && d.SQL == queryKubernetesInputStage {
		tr.armed.Store(true)
	}
	return context.WithValue(ctx, kubernetesInputCommitMarker{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}

// TraceQueryEnd waits only after a successful native COMMIT, then restores eligibility before the second owner lock.
// TraceQueryEnd ждёт лишь после successful native COMMIT, затем восстанавливает eligibility до second owner lock.
func (tr *kubernetesInputPreludeTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(kubernetesInputCommitMarker{}).(bool)
	if !commit || d.Err != nil || !tr.armed.CompareAndSwap(true, false) || !tr.fired.CompareAndSwap(false, true) {
		return
	}
	tr.hits.Add(1)
	timer := time.NewTimer(time.Until(tr.until.Add(25 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		tr.restoreErr = ctx.Err()
		return
	case <-timer.C:
	}
	tr.restoreErr = tr.restore()
}

// kubernetesInputFixtureOperation resolves the real protected operation map instead of inventing a fixture permission.
// kubernetesInputFixtureOperation разрешает actual protected operation map, не выдумывает fixture permission.
func kubernetesInputFixtureOperation(t *testing.T, operations map[string]string, method string) string {
	t.Helper()
	for operation, registered := range operations {
		if registered == method {
			return operation
		}
	}
	t.Fatal("production input owner operation missing")
	return ""
}

// TestKubernetesInputPreludeDeadlineOwnerComponent retains the first finite horizon across a delayed prelude and restored alternative.
// Queued bytes and CLAIMED/ACKNOWLEDGED recovery must fail closed; a later fresh request preserves the same immutable intent without PUT/replay.
// TestKubernetesInputPreludeDeadlineOwnerComponent сохраняет first finite horizon после delayed prelude и restored alternative.
// Queued bytes и CLAIMED/ACKNOWLEDGED recovery закрываются; поздний fresh request сохраняет immutable intent без PUT/replay.
func TestKubernetesInputPreludeDeadlineOwnerComponent(t *testing.T) {
	for _, state := range []string{"QUEUED", "CLAIMED", "ACKNOWLEDGED"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
			defer cancel()
			r, service, broker, gateway, binding, _, admission, _, actor := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
			stream := admission.Stream.Ref
			actor.Permission = "platform.integrations.kubernetes.input.get"
			selected, err := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream})
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("synthetic prelude input")
			defer clear(payload)
			sum := sha256.Sum256(payload)
			actor.Permission = "platform.integrations.kubernetes.input.submit"
			const key = "input-prelude-original"
			staged, err := service.SubmitKubernetesStreamInput(ctx, actor, value.Mutation{IdempotencyKey: key, ExpectedVersion: &selected.View.Version}, command.KubernetesStreamInputSubmit{InvocationRef: binding.InvocationRef, StreamRef: stream, Channel: "STDIN", Payload: payload, PayloadDigest: hex.EncodeToString(sum[:])})
			if err != nil || staged.Submission == nil {
				t.Fatal("queue actual non-FILE stage", err)
			}
			gateway.Permission = kubernetesInputFixtureOperation(t, controlplaneclient.IntegrationGatewayOperations(), cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInputFrame_FullMethodName)
			resolvedGateway, err := r.ResolvePrincipal(ctx, gateway)
			if err != nil {
				t.Fatal("resolve input prelude gateway", err)
			}
			if state != "QUEUED" {
				delivered, claimErr := r.ClaimManagedKubernetesInputFrame(ctx, resolvedGateway, binding, stream)
				if claimErr != nil || delivered.Frame == nil {
					t.Fatal("claim initial exact stage", claimErr)
				}
				if state == "ACKNOWLEDGED" {
					broker.Permission = kubernetesInputFixtureOperation(t, controlplaneclient.SecretBrokerOperations(), cp.ManagedKubernetesOwnerService_BeginManagedKubernetesFrame_FullMethodName)
					begin := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: delivered.Frame.Sequence, Channel: "STDIN", PayloadDigest: delivered.Frame.PayloadDigest, ByteCount: delivered.Frame.ByteCount}
					admitted, beginErr := r.BeginManagedKubernetesFrame(ctx, broker, binding, begin)
					if beginErr != nil || admitted.Disposition != enum.KubernetesFrameSend {
						clear(delivered.Frame.Payload)
						t.Fatal("admit original input", beginErr)
					}
					broker.Permission = kubernetesInputFixtureOperation(t, controlplaneclient.SecretBrokerOperations(), cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesFrame_FullMethodName)
					if _, completeErr := r.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: begin.Sequence, PayloadDigest: begin.PayloadDigest}); completeErr != nil {
						clear(delivered.Frame.Payload)
						t.Fatal("acknowledge original input", completeErr)
					}
				}
				clear(delivered.Frame.Payload)
			}
			current, err := r.resolveScope(ctx, resolvedGateway)
			if err != nil {
				t.Fatal(err)
			}
			until := time.Now().Add(3 * time.Second)
			tag, err := r.pool.Exec(ctx, kubernetesOutputACLDeadlineSQL, current.organizationID, until)
			if err != nil || tag.RowsAffected() == 0 {
				t.Fatal("bound original ACL", err)
			}
			trace := &kubernetesInputPreludeTrace{until: until, restore: func() error {
				_, err := r.pool.Exec(ctx, kubernetesInputACLRestoreSQL, current.organizationID)
				return err
			}}
			config := r.pool.Config().Copy()
			config.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			target := *r
			target.pool = pool
			store := &kubernetesInputDisclosureStore{Store: r.objects, afterClose: func() error { return nil }}
			target.objects = store
			result, claimErr := target.ClaimManagedKubernetesInputFrame(ctx, resolvedGateway, binding, stream)
			if result.Frame != nil {
				clear(result.Frame.Payload)
			}
			if trace.hits.Load() != 1 || trace.restoreErr != nil || ctx.Err() != nil || !errors.Is(claimErr, errs.ErrForbidden) || result.Frame != nil || store.gets.Load() != 0 {
				t.Fatal("expired first horizon escaped through data/recovery", claimErr, trace.restoreErr)
			}
			actor.Permission = "platform.integrations.kubernetes.input.get"
			readback, err := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream, SubmissionIdempotencyKey: key})
			if err != nil || readback.Submission == nil || readback.Submission.Ref != staged.Submission.Ref || string(readback.Submission.State) != state {
				t.Fatal("delayed prelude changed immutable input intent", err)
			}
			fresh, err := target.ClaimManagedKubernetesInputFrame(ctx, resolvedGateway, binding, stream)
			if err != nil {
				t.Fatal("fresh original intent readback", err)
			}
			switch state {
			case "QUEUED":
				if fresh.Frame == nil || fresh.Disposition != enum.KubernetesInputFrameDeliver || store.gets.Load() != 1 {
					t.Fatal("fresh request failed single original delivery")
				}
				clear(fresh.Frame.Payload)
				again, againErr := target.ClaimManagedKubernetesInputFrame(ctx, resolvedGateway, binding, stream)
				if againErr != nil || again.Frame != nil || again.Disposition != enum.KubernetesInputFrameUnknown || store.gets.Load() != 1 {
					if again.Frame != nil {
						clear(again.Frame.Payload)
					}
					t.Fatal("original delivery replayed", againErr)
				}
			case "CLAIMED":
				if fresh.Frame != nil || fresh.Disposition != enum.KubernetesInputFrameUnknown || store.gets.Load() != 0 {
					t.Fatal("claimed recovery replayed bytes")
				}
			case "ACKNOWLEDGED":
				if fresh.Frame != nil || fresh.Disposition != enum.KubernetesInputFrameNone || store.gets.Load() != 0 {
					t.Fatal("acknowledged recovery reopened bytes")
				}
			}
			gateway.Permission = kubernetesInputFixtureOperation(t, controlplaneclient.IntegrationGatewayOperations(), cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName)
			if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
				t.Fatal("close actual owner graph", err)
			}
		})
	}
}
