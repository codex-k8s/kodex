package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgtype"
)

// kubernetesEmptyInputFixtureDigest binds the metadata-only fixture input '{}', not real target input.
// kubernetesEmptyInputFixtureDigest связывает metadata-only fixture input '{}', не real target input.
const kubernetesEmptyInputFixtureDigest = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

// assertKubernetesActiveStreamReadback installs metadata-only stream fixtures under an actual
// owner claim. It verifies idle authority readback, never proves a transport handshake or effect admission.
// assertKubernetesActiveStreamReadback создаёт metadata-only stream fixtures под actual owner claim.
// Проверяет idle authority readback, не доказывает transport handshake либо effect admission.
func assertKubernetesActiveStreamReadback(t *testing.T, ctx context.Context, repository *Repository, broker value.Principal, binding value.ManagedKubernetesBinding, snapshotDigest string) {
	t.Helper()
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal("encode native stream binding", err)
	}
	const streamRef = "kst_native_readback"
	const install = `WITH tick AS MATERIALIZED(SELECT clock_timestamp() AS stamp),
 plan AS(INSERT INTO control_plane.kubernetes_execution_plans
 (ref,organization_id,invocation_id,binding_digest,snapshot_digest,proposal_digest,plan_digest,child_count,hard_deadline,created_at)
 SELECT 'kpl_native_readback',i.organization_id,i.id,$2,$3,$3,$3,1,tick.stamp+interval '5 minutes',tick.stamp
 FROM control_plane.integration_invocations i CROSS JOIN tick WHERE i.ref=$1 RETURNING *),
 child AS(INSERT INTO control_plane.kubernetes_execution_children
 (ref,organization_id,plan_id,ordinal,operation,risk,approval_policy,capability_key,grant_id,grant_version,grant_scope_digest,input_json,input_digest,observation_json)
 SELECT 'kch_native_readback',p.organization_id,p.id,0,'kubernetes.logs','READ','NONE',g.capability_key,g.id,g.version,g.resource_scope_digest,'{}',encode(public.digest(convert_to('{}','UTF8'),'sha256'),'hex'),'{"resource":{"group":"","version":"v1","resource":"pods","namespace":"review","name":"synthetic","namespaced":true}}'::jsonb
 FROM plan p JOIN control_plane.integration_invocations i ON i.id=p.invocation_id
 JOIN control_plane.integration_grants g ON g.connection_id=i.connection_id AND g.organization_id=i.organization_id AND g.capability_key='kubernetes.logs' AND g.enabled RETURNING *),
 effect AS(INSERT INTO control_plane.kubernetes_execution_effects
 (ref,organization_id,child_id,generation,state,admission_expires_at,created_at)
 SELECT 'kef_native_readback',c.organization_id,c.id,1,'ATTEMPTING',tick.stamp+interval '30 seconds',tick.stamp FROM child c CROSS JOIN tick RETURNING *)
 INSERT INTO control_plane.kubernetes_execution_streams
 (ref,organization_id,effect_id,generation,state,hard_deadline,maximum_frame_bytes,maximum_total_bytes,input_channels,output_channels,created_at)
 SELECT 'kst_native_readback',e.organization_id,e.id,$4,'PREPARED',tick.stamp+interval '5 minutes',32768,65536,ARRAY['STDIN','TERMINAL_RESIZE'],ARRAY['STDOUT'],tick.stamp FROM effect e CROSS JOIN tick`
	if _, err := repository.pool.Exec(ctx, install, binding.InvocationRef, bindingDigest, snapshotDigest, binding.Generation); err != nil {
		t.Fatal("install native readback metadata", err)
	}
	base, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil {
		t.Fatal("read original owner before handshake", err)
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshotDigest, BindingDigest: bindingDigest, ChildInputDigest: kubernetesEmptyInputFixtureDigest, EffectRef: "kef_native_readback", EffectGeneration: 1, StreamRef: streamRef, StreamGeneration: binding.Generation, Operation: "kubernetes.logs", TargetRef: base.Target.Ref, TargetRevision: base.Target.Revision}
	handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: base.Target.Ref, TransportGeneration: base.Target.Revision, Protocol: managedkubernetes.HTTPLogsV1, HTTPStatus: 200}
	handshakeDigest, err := handshake.Digest()
	if err != nil {
		t.Fatal("encode metadata-only handshake fixture", err)
	}
	openBroker := broker
	openBroker.Permission = "platform.runtime.kubernetes.stream.open"
	opening := command.ManagedKubernetesOpenStream{EffectRef: origin.EffectRef, EffectGeneration: 1, StreamRef: streamRef, HandshakeReceipt: handshake, HandshakeReceiptDigest: handshakeDigest}
	faultRepository, fault := kubernetesAuthorityFaultRepository(t, ctx, repository, queryKubernetesStreamRecord, pgtype.TimestamptzOID)
	if _, err := faultRepository.OpenManagedKubernetesStream(ctx, openBroker, binding, opening); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
		t.Fatal("native successful handshake Scan failure became authority denial", err)
	}
	for replay := 0; replay < 2; replay++ {
		grant, err := repository.OpenManagedKubernetesStream(ctx, openBroker, binding, opening)
		if err != nil || grant.Ref != streamRef || grant.Generation != binding.Generation {
			t.Fatal("owner handshake was not idempotent", err)
		}
	}
	foreign := opening
	foreign.HandshakeReceipt.Origin.EffectRef = "kef_foreign_fixture"
	foreign.HandshakeReceiptDigest, _ = foreign.HandshakeReceipt.Digest()
	if _, err := repository.OpenManagedKubernetesStream(ctx, openBroker, binding, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign handshake origin entered active owner stream", err)
	}
	current, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding, streamRef)
	if err != nil || current.PlanRef != "kpl_native_readback" || current.PlanRevision != 1 || !current.ReconcileOnly || current.Digest != snapshotDigest {
		t.Fatal("active stream readback lost immutable original claim", err)
	}
	if _, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding, "kst_foreign_readback"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign stream received current owner authority", err)
	}
	for _, selected := range []struct {
		query string
		oid   uint32
	}{{queryKubernetesExecutionPlanRead, pgtype.BoolOID}, {queryKubernetesExecutionActiveStream, pgtype.TimestamptzOID}} {
		faultRepository, fault := kubernetesAuthorityFaultRepository(t, ctx, repository, selected.query, selected.oid)
		if _, err := faultRepository.ResolveManagedKubernetesOperation(ctx, broker, binding, streamRef); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
			t.Fatal("successful native stream Scan failure became denial", err)
		}
	}
	assertKubernetesFrameOwnerAccounting(t, ctx, repository, broker, binding, streamRef)
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_grants grant_binding SET enabled=false,version=version+1
 FROM control_plane.kubernetes_execution_children child WHERE child.grant_id=grant_binding.id AND child.ref='kch_native_readback'`); err != nil {
		t.Fatal("revoke exact child namespace grant", err)
	}
	if _, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding); err != nil {
		t.Fatal("child grant revoke unexpectedly revoked independent parent identity grant", err)
	}
	assertKubernetesOutputClosedReadback(t, ctx, repository, binding, streamRef)
	if _, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding, streamRef); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("idle stream ignored revoked exact child grant", err)
	}
	frameBroker := broker
	frameBroker.Permission = "platform.runtime.kubernetes.frame.begin"
	frame := command.ManagedKubernetesBeginFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameOutput, NativeAuthorizedUntil: time.Now().Add(4 * time.Second), Sequence: 2, Channel: "STDOUT", PayloadDigest: kubernetesOutputFixtureDigest(), ByteCount: 32768}
	if _, err := repository.BeginManagedKubernetesFrame(ctx, frameBroker, binding, frame); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("OUTPUT replay ignored revoked child namespace grant", err)
	}
	terminal := managedkubernetes.TerminalReceipt{Origin: origin, HandshakeDigest: handshakeDigest, Protocol: managedkubernetes.HTTPLogsV1, TransportJoined: true, LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "TRANSPORT_LOST"}
	terminalDigest, err := terminal.Digest()
	if err != nil {
		t.Fatal("encode metadata-only stream terminal", err)
	}
	closing := command.ManagedKubernetesCloseStream{StreamRef: streamRef, ExpectedState: enum.KubernetesStreamActive, TerminalState: enum.KubernetesStreamLost, RemoteOutcome: enum.KubernetesEffectUnknown, TransportJoined: true, TerminalReceipt: terminal, TerminalReceiptDigest: terminalDigest}
	closeBroker := broker
	closeBroker.Permission = "platform.runtime.kubernetes.stream.close"
	var beforeClose, afterClose string
	if repository.pool.QueryRow(ctx, "SELECT md5(to_jsonb(stream)::text) FROM control_plane.kubernetes_execution_streams stream WHERE ref=$1", streamRef).Scan(&beforeClose) != nil {
		t.Fatal("read stream before native close fault")
	}
	failedCloser, closeFault := kubernetesAuthorityFaultRepository(t, ctx, repository, queryKubernetesStreamRecord, pgtype.TimestamptzOID)
	if _, err := failedCloser.CloseManagedKubernetesStream(ctx, closeBroker, binding, closing); !errors.Is(err, errs.ErrUnavailable) || closeFault.fired.Load() != 1 {
		t.Fatal("native close Scan failure became revocation", err)
	}
	if repository.pool.QueryRow(ctx, "SELECT md5(to_jsonb(stream)::text) FROM control_plane.kubernetes_execution_streams stream WHERE ref=$1", streamRef).Scan(&afterClose) != nil || afterClose != beforeClose {
		t.Fatal("infrastructure failure changed stream state or receipt")
	}
	for replay := 0; replay < 2; replay++ {
		closed, err := repository.CloseManagedKubernetesStream(ctx, closeBroker, binding, closing)
		if err != nil || closed.State != enum.KubernetesStreamRevoked || closed.RemoteOutcome != enum.KubernetesEffectUnknown {
			t.Fatal("late close reopened revoked child grant or lost original outcome", err)
		}
	}

	if _, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding, streamRef); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("closed stream reused live outer lease", err)
	}
	lateBroker := broker
	lateBroker.Permission = "platform.runtime.kubernetes.frame.complete"
	assertKubernetesOutputClosedReadback(t, ctx, repository, binding, streamRef)
	late := command.ManagedKubernetesCompleteFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameOutput, Sequence: 2, PayloadDigest: kubernetesOutputFixtureDigest()}
	if _, err := repository.CompleteManagedKubernetesFrame(ctx, lateBroker, binding, late); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unknown private OUTPUT stage became a client ACK", err)
	}
	late.Direction, late.Sequence, late.PayloadDigest = enum.KubernetesFrameInput, 3, strings.Repeat("a", 64)
	if sequence, err := repository.CompleteManagedKubernetesFrame(ctx, lateBroker, binding, late); err != nil || sequence != 3 {
		t.Fatal("late exact INPUT receipt was lost", err)
	}
	late.Direction, late.Sequence = enum.KubernetesFrameOutput, 1
	sum := sha256.Sum256([]byte("body"))
	late.PayloadDigest = hex.EncodeToString(sum[:])
	if sequence, err := repository.CompleteManagedKubernetesFrame(ctx, lateBroker, binding, late); err != nil || sequence != 1 {
		t.Fatal("late accepted OUTPUT ACK receipt was lost", err)
	}
	readback, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil || readback.PlanDigest != snapshotDigest || !readback.ReconcileOnly {
		t.Fatal("historical plan readback granted or lost reconciliation", err)
	}
	if _, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding, strings.Repeat("x", 129)); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("unbounded stream locator reached owner graph", err)
	}
}
