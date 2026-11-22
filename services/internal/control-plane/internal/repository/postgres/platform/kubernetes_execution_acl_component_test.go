package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesEffectACLTraceKey carries only a normalized commit marker between native driver callbacks.
// kubernetesEffectACLTraceKey переносит только normalized commit marker между native driver callbacks.
type kubernetesEffectACLTraceKey struct{}

// kubernetesEffectACLTrace delays a successful native COMMIT after the exact owner read or intent; it does not fake a transaction error or alter PostgreSQL time.
// kubernetesEffectACLTrace задерживает successful native COMMIT после exact owner read либо intent; не подделывает transaction error и не меняет PostgreSQL time.
type kubernetesEffectACLTrace struct {
	target string
	until  time.Time
	armed  atomic.Bool
	hits   atomic.Int32
}

// TraceQueryStart identifies the exact owner statement without retaining SQL arguments or private material.
// TraceQueryStart определяет exact owner statement без сохранения SQL arguments либо private material.
func (tr *kubernetesEffectACLTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if d.SQL == tr.target {
		tr.armed.Store(true)
	}
	return context.WithValue(ctx, kubernetesEffectACLTraceKey{}, strings.EqualFold(strings.TrimSpace(d.SQL), "commit"))
}

// TraceQueryEnd waits past the actual permission deadline only after successful native commit, leaving the caller context alive.
// TraceQueryEnd ждёт actual permission deadline лишь после successful native commit, сохраняя живой caller context.
func (tr *kubernetesEffectACLTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	commit, _ := ctx.Value(kubernetesEffectACLTraceKey{}).(bool)
	if !commit || d.Err != nil || !tr.armed.CompareAndSwap(true, false) {
		return
	}
	tr.hits.Add(1)
	timer := time.NewTimer(time.Until(tr.until.Add(25 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// TestKubernetesEffectACLDeadlineOwnerComponent exercises real original-actor ACL expiry before dispatch and after successful commit, including an approved child and immutable intent replay after a restored alternative.
// TestKubernetesEffectACLDeadlineOwnerComponent проверяет real original-actor ACL expiry до dispatch и после successful commit, включая approved child и immutable intent replay после восстановления alternative.
func TestKubernetesEffectACLDeadlineOwnerComponent(t *testing.T) {
	for _, scenario := range []string{"resolve", "begin", "child", "restored"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			r, _, broker, _, binding, plan, snapshot, _ := newKubernetesApprovedPlanFixture(t, ctx)
			current, err := r.resolveScope(ctx, broker)
			if err != nil {
				t.Fatal("resolve original owner scope", err)
			}
			until := time.Now().Add(3 * time.Second)
			if tag, err := r.pool.Exec(ctx, kubernetesOutputACLDeadlineSQL, current.organizationID, until); err != nil || tag.RowsAffected() == 0 {
				t.Fatal("bound disposable ACL", err)
			}
			tr := &kubernetesEffectACLTrace{until: until, target: queryKubernetesEffectInsert}
			if scenario == "resolve" {
				tr.target = queryKubernetesExecutionPlanRead
			}
			config := r.pool.Config()
			config.ConnConfig.Tracer = tr
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal("create native ACL trace pool", err)
			}
			defer pool.Close()
			if err := pool.Ping(ctx); err != nil {
				t.Fatal("connect native ACL trace pool", err)
			}
			copy := *r
			copy.pool = pool
			opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: plan.Children[0].Ref, ChildInputDigest: plan.Children[0].InputDigest}
			if scenario == "child" {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				owner, _, _, _, err := copy.readKubernetesChildGateOwnerTx(ctx, tx, current, binding.InvocationRef)
				if err != nil {
					t.Fatal("read original child owner", err)
				}
				if owner.lease.ExpiresAt.After(until) {
					t.Error("child owner dropped finite original ACL deadline")
				}
				return
			}
			if scenario == "restored" {
				broker.Permission = "platform.runtime.kubernetes.effect.begin"
				admitted, err := r.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
				if err != nil || admitted.ReconcileOnly || admitted.Stream == nil {
					t.Fatal("first original admission", err)
				}
				if admitted.ExpiresAt.After(until) || admitted.Stream.ExpiresAt.After(until) {
					t.Error("original intent exceeded finite permission")
				}
				bindingDigest, _ := managedkubernetes.BindingDigest(binding)
				child := plan.Children[0]
				pin := child.NodeTransport
				if pin == nil {
					t.Fatal("missing original node pin")
				}
				handshake := managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: bindingDigest, ChildInputDigest: child.InputDigest, EffectRef: admitted.Ref, EffectGeneration: admitted.Generation, StreamRef: admitted.Stream.Ref, StreamGeneration: binding.Generation, Operation: child.Operation, TargetRef: snapshot.Target.Ref, TargetRevision: snapshot.Target.Revision}, TransportRef: pin.Ref, TransportGeneration: pin.Generation, Protocol: string(pin.Protocol), HTTPStatus: 101, NegotiatedProtocol: pin.Protocol.WireProtocol(), PeerSPKISHA256: pin.ServerPublicKeySHA256}
				handshakeDigest, err := handshake.Digest()
				if err != nil {
					t.Fatal(err)
				}
				broker.Permission = "platform.runtime.kubernetes.stream.open"
				if _, err := r.OpenManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesOpenStream{StreamRef: admitted.Stream.Ref, EffectRef: admitted.Ref, EffectGeneration: admitted.Generation, HandshakeReceipt: handshake, HandshakeReceiptDigest: handshakeDigest}); err != nil {
					t.Fatal("record original native handshake", err)
				}
				timer := time.NewTimer(time.Until(until.Add(25 * time.Millisecond)))
				defer timer.Stop()
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-timer.C:
				}
				if _, err := r.pool.Exec(ctx, "UPDATE control_plane.access_bindings SET valid_until=NULL WHERE organization_id=$1::uuid AND state='ACTIVE'", current.organizationID); err != nil {
					t.Fatal("restore disposable alternative", err)
				}
				broker.Permission = "platform.runtime.kubernetes.effect.begin"
				replay, err := r.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
				if err != nil && !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("restored original intent rejoin failed infrastructure", err)
				}
				if err == nil && (!replay.ReconcileOnly || replay.Ref != admitted.Ref || replay.Generation != admitted.Generation) {
					t.Error("restored ACL created another dispatch")
				}
				var count int
				var original time.Time
				if err := r.pool.QueryRow(ctx, "SELECT count(*)::int,min(admission_expires_at) FROM control_plane.kubernetes_execution_effects WHERE organization_id=$1::uuid AND ref=$2", current.organizationID, admitted.Ref).Scan(&count, &original); err != nil || count != 1 || original.After(until) {
					t.Error("restored ACL extended original intent", err)
				}
				zero := int32(0)
				terminal := managedkubernetes.TerminalReceipt{Origin: handshake.Origin, HandshakeDigest: handshakeDigest, Protocol: handshake.Protocol, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", TransportJoined: true, ExitCode: &zero, ReasonCode: "REMOTE_EXIT"}
				terminalDigest, err := terminal.Digest()
				if err != nil {
					t.Fatal(err)
				}
				broker.Permission = "platform.runtime.kubernetes.stream.close"
				closed, err := r.CloseManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesCloseStream{StreamRef: admitted.Stream.Ref, ExpectedState: enum.KubernetesStreamActive, TerminalState: enum.KubernetesStreamCompleted, RemoteOutcome: enum.ManagedKubernetesEffectOutcome("SUCCEEDED"), TransportJoined: true, ObservedExitCode: &zero, TerminalReceipt: terminal, TerminalReceiptDigest: terminalDigest})
				if err != nil || closed.RemoteOutcome != "SUCCEEDED" {
					t.Fatal("late native outcome was erased", err)
				}
				built, err := managedkubernetes.BuildStreamEffectResult(terminal)
				if err != nil {
					t.Fatal(err)
				}
				completion := command.ManagedKubernetesCompleteEffect{EffectRef: admitted.Ref, EffectGeneration: admitted.Generation, ChildInputDigest: child.InputDigest, StreamRef: admitted.Stream.Ref, Outcome: enum.ManagedKubernetesEffectOutcome(built.Outcome), ResultJSON: built.ResultJSON, ResultDigest: built.ResultDigest, ReasonCode: built.ReasonCode}
				broker.Permission = "platform.runtime.kubernetes.effect.complete"
				receipt, err := r.CompleteManagedKubernetesEffect(ctx, broker, binding, completion)
				if err != nil || receipt.Applied || !receipt.AuthorizedUntil.IsZero() {
					t.Fatal("restored ACL republished old effect credit", err)
				}
				replayed, err := r.CompleteManagedKubernetesEffect(ctx, broker, binding, completion)
				if err != nil || replayed != receipt {
					t.Fatal("late completion ACK recovery lost original fact", err)
				}
				var outcome, submission string
				var applied bool
				if r.pool.QueryRow(ctx, "SELECT fact.outcome,fact.submission_digest,fact.applied FROM control_plane.kubernetes_execution_completions fact JOIN control_plane.kubernetes_execution_effects effect ON effect.id=fact.effect_id WHERE effect.organization_id=$1::uuid AND effect.ref=$2", current.organizationID, admitted.Ref).Scan(&outcome, &submission, &applied) != nil || outcome != "SUCCEEDED" || submission != built.ResultDigest || applied {
					t.Fatal("native truth or immutable receipt changed after ACL expiry")
				}
				return
			}
			if scenario == "resolve" {
				broker.Permission = "platform.runtime.kubernetes.resolve"
				got, err := copy.ResolveManagedKubernetesOperation(ctx, broker, binding)
				if !errors.Is(err, errs.ErrForbidden) || !got.ExpiresAt.IsZero() || got.Digest != "" {
					t.Error("expired ACL escaped Resolve commit", err)
				}
			} else {
				broker.Permission = "platform.runtime.kubernetes.effect.begin"
				got, err := copy.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
				if !errors.Is(err, errs.ErrForbidden) || got.Ref != "" || !got.ExpiresAt.IsZero() {
					t.Error("expired ACL escaped Begin commit", err)
				}
			}
			if tr.hits.Load() != 1 || time.Now().Before(until) || ctx.Err() != nil {
				t.Fatal("native commit expiry fixture did not reach boundary")
			}
		})
	}
}

// TestKubernetesNonstreamCreditOwnerComponent retains a real original nonstream mutation receipt after finite authority expires, refuses redispatch under a restored alternative, and recovers one lost completion ACK without output credit.
// TestKubernetesNonstreamCreditOwnerComponent сохраняет original nonstream mutation receipt после finite authority expiry, запрещает redispatch при restored alternative и восстанавливает lost completion ACK без output credit.
func TestKubernetesNonstreamCreditOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	repository, _, broker, _, binding, plan, _, _ := newKubernetesApprovedOperationPlanFixture(t, ctx, "kubernetes.replace")
	current, err := repository.resolveScope(ctx, broker)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	if tag, err := repository.pool.Exec(ctx, kubernetesOutputACLDeadlineSQL, current.organizationID, until); err != nil || tag.RowsAffected() == 0 {
		t.Fatal("bound original mutation ACL", err)
	}
	child := plan.Children[0]
	opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	admitted, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
	if err != nil || admitted.Stream != nil || admitted.ReconcileOnly || admitted.ExpiresAt.After(until) {
		t.Fatal("admit one finite resource mutation", err)
	}
	timer := time.NewTimer(time.Until(until.Add(25 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	if _, err := repository.pool.Exec(ctx, "UPDATE control_plane.access_bindings SET valid_until=NULL WHERE organization_id=$1::uuid AND state='ACTIVE'", current.organizationID); err != nil {
		t.Fatal("restore disposable mutation alternative", err)
	}
	rejoin, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
	if err != nil && !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("restored mutation rejoin failed infrastructure", err)
	}
	if err == nil && (!rejoin.ReconcileOnly || rejoin.Ref != admitted.Ref || rejoin.Generation != admitted.Generation || rejoin.AcceptedResult != nil) {
		t.Fatal("restored permission admitted another mutation")
	}
	raw := []byte(`{"metadata":{"name":"settings"}}`)
	digest := sha256.Sum256(raw)
	input := command.ManagedKubernetesCompleteEffect{EffectRef: admitted.Ref, EffectGeneration: admitted.Generation, ChildInputDigest: child.InputDigest, Outcome: enum.KubernetesEffectSucceeded, ResultJSON: raw, ResultDigest: hex.EncodeToString(digest[:])}
	broker.Permission = "platform.runtime.kubernetes.effect.complete"
	wrong := binding
	wrong.Fence = "foreign-mutation-fence"
	if _, err := repository.CompleteManagedKubernetesEffect(ctx, broker, wrong, input); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("foreign binding recorded native mutation", err)
	}
	receipt, err := repository.CompleteManagedKubernetesEffect(ctx, broker, binding, input)
	if err != nil || receipt.Applied || !receipt.AuthorizedUntil.IsZero() || receipt.SubmissionDigest != input.ResultDigest {
		t.Fatal("late native mutation was lost or gained disclosure", err)
	}
	replay, err := repository.CompleteManagedKubernetesEffect(ctx, broker, binding, input)
	if err != nil || replay != receipt {
		t.Fatal("lost nonstream completion ACK changed durable outcome", err)
	}
	changed := input
	changed.Outcome = enum.KubernetesEffectUnknown
	changed.ReasonCode = managedkubernetes.ReasonTransportUnknown
	if _, err := repository.CompleteManagedKubernetesEffect(ctx, broker, binding, changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("late mutation replay changed historical truth", err)
	}
	var count int
	var outcome, submission, result string
	var applied bool
	var original time.Time
	err = repository.pool.QueryRow(ctx, `SELECT (SELECT count(*)::int FROM control_plane.kubernetes_execution_effects same WHERE same.child_id=effect.child_id),effect.admission_expires_at,fact.outcome,fact.submission_digest,fact.result_json::text,fact.applied FROM control_plane.kubernetes_execution_completions fact JOIN control_plane.kubernetes_execution_effects effect ON effect.id=fact.effect_id WHERE effect.organization_id=$1::uuid AND effect.ref=$2`, current.organizationID, admitted.Ref).Scan(&count, &original, &outcome, &submission, &result, &applied)
	if err != nil || count != 1 || original.After(until) || outcome != "SUCCEEDED" || submission != input.ResultDigest || applied || result != "{}" {
		t.Fatal("late native mutation fact or single-attempt boundary changed", err)
	}
}
