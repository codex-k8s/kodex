package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertKubernetesPreparedStreamFailure uses actual owner-created stream reservations and synthetic
// broker receipts to prove failed handshake and lost Open ACK closure. It proves owner persistence,
// not native TLS, transport join or mTLS/policy authentication of the fixture receipt.
// assertKubernetesPreparedStreamFailure использует actual owner stream reservations и synthetic
// broker receipts для failure handshake/lost Open ACK closure. Доказывает owner persistence,
// не native TLS, transport join либо mTLS/policy authentication fixture receipt.
func assertKubernetesPreparedStreamFailure(t *testing.T, ctx context.Context, repository *Repository, broker value.Principal, claim func(string, string, map[string]any) value.ManagedKubernetesBinding, mode string) {
	t.Helper()
	captured := mode == "LOST_OPEN_ACK" || mode == "ACTIVE_OWNER_LOST"
	ownerLost := mode == "PREPARED_OWNER_LOST" || mode == "ACTIVE_OWNER_LOST"
	key := "kube-stream-" + mode
	binding := claim("kubernetes.logs", key, map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "options": map[string]any{"container": "main", "timeoutSeconds": 120}})
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil {
		t.Fatal("resolve prepared stream", err)
	}
	candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17", Namespaced: true}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
	proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.children.prepare"
	plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
	if err != nil {
		t.Fatal("prepare actual stream child", err)
	}
	child := plan.Children[0]
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	admission, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: 1, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest})
	if err != nil || admission.Stream == nil || admission.ReconcileOnly {
		t.Fatal("reserve actual prepared stream", err)
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: bindingDigest, ChildInputDigest: child.InputDigest, EffectRef: admission.Ref, EffectGeneration: admission.Generation, StreamRef: admission.Stream.Ref, StreamGeneration: binding.Generation, Operation: "kubernetes.logs", TargetRef: snapshot.Target.Ref, TargetRevision: snapshot.Target.Revision}
	terminal := managedkubernetes.TerminalReceipt{Origin: origin, Protocol: managedkubernetes.HTTPLogsV1, LocalState: "LOST", RemoteOutcome: "UNKNOWN", TransportJoined: true, ReasonCode: "HANDSHAKE_FAILED"}
	closing := command.ManagedKubernetesCloseStream{StreamRef: origin.StreamRef, ExpectedState: enum.KubernetesStreamPrepared, TerminalState: enum.KubernetesStreamLost, RemoteOutcome: enum.KubernetesEffectUnknown, TransportJoined: true}
	if captured {
		observed := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: origin.TargetRef, TransportGeneration: origin.TargetRevision, Protocol: managedkubernetes.HTTPLogsV1, HTTPStatus: 200}
		digest, err := observed.Digest()
		if err != nil {
			t.Fatal(err)
		}
		closing.ObservedHandshake = &observed
		closing.ExpectedState = enum.KubernetesStreamActive
		terminal.HandshakeDigest = digest
		terminal.ReasonCode = "TRANSPORT_LOST"
	}
	broker.Permission = "platform.runtime.kubernetes.resolve"
	if _, cleanupErr := repository.ResolveManagedKubernetesCleanup(ctx, broker, binding, origin.StreamRef); !errors.Is(cleanupErr, errs.ErrForbidden) {
		t.Fatal("live prepared stream gained cleanup proof", cleanupErr)
	}
	if mode == "ACTIVE_OWNER_LOST" {
		broker.Permission = "platform.runtime.kubernetes.stream.open"
		handshakeDigest, _ := closing.ObservedHandshake.Digest()
		if _, openErr := repository.OpenManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesOpenStream{StreamRef: origin.StreamRef, EffectRef: origin.EffectRef, EffectGeneration: origin.EffectGeneration, HandshakeReceiptDigest: handshakeDigest, HandshakeReceipt: *closing.ObservedHandshake}); openErr != nil {
			t.Fatal("record actual owner handshake", openErr)
		}
		closing.ObservedHandshake = nil
	}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	recovered, historyErr := repository.BeginManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: 1, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest})
	if historyErr != nil || !recovered.ReconcileOnly || recovered.Stream != nil || recovered.History == nil || recovered.History.Ref != origin.StreamRef || recovered.History.Terminal != nil || (recovered.History.Handshake != nil) != (mode == "ACTIVE_OWNER_LOST") {
		t.Fatal("same live lease history invented dispatch or missing handshake", mode, historyErr)
	}
	if ownerLost {
		closing.TransportJoined = false
		terminal.TransportJoined = false
		if mode == "PREPARED_OWNER_LOST" {
			terminal.ReasonCode = "OWNER_STATE_LOST"
		} else {
			terminal.ReasonCode = "OUTPUT_LOST"
		}
	}
	closing.TerminalReceipt = terminal
	closing.TerminalReceiptDigest, err = terminal.Digest()
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.stream.close"
	for replay := 0; replay < 2; replay++ {
		receipt, err := repository.CloseManagedKubernetesStream(ctx, broker, binding, closing)
		if err != nil || receipt.State != enum.KubernetesStreamLost || receipt.RemoteOutcome != enum.KubernetesEffectUnknown {
			t.Fatal("prepared handshake failure could not close honestly", captured, err)
		}
	}
	var state string
	var handshake string
	if repository.pool.QueryRow(ctx, `SELECT state,coalesce(handshake_digest,'') FROM control_plane.kubernetes_execution_streams WHERE ref=$1`, origin.StreamRef).Scan(&state, &handshake) != nil || state != "LOST" || (handshake != "") != captured {
		t.Fatal("late witness fabricated a handshake or reopened stream")
	}
	if mode == "LOST_OPEN_ACK" {
		damaged := closing
		observed := *closing.ObservedHandshake
		observed.TransportGeneration++
		damaged.ObservedHandshake = &observed
		if _, err := repository.CloseManagedKubernetesStream(ctx, broker, binding, damaged); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("late witness changed immutable transport pin", err)
		}
	}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	historical, historyErr := repository.BeginManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: 1, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest})
	if historyErr != nil || historical.History == nil || historical.History.Terminal == nil || historical.History.Terminal.RemoteOutcome != "UNKNOWN" || historical.History.Terminal.TransportJoined != !ownerLost {
		t.Fatal("terminal history changed native uncertainty or join", mode, historyErr)
	}
	broker.Permission = "platform.runtime.kubernetes.resolve"
	cleanup, cleanupErr := repository.ResolveManagedKubernetesCleanup(ctx, broker, binding, origin.StreamRef)
	if cleanupErr != nil || !cleanup.ExecutionClosed || cleanup.BindingDigest != bindingDigest || cleanup.InvocationRef != binding.InvocationRef || cleanup.SnapshotDigest != snapshot.Digest {
		t.Fatal("closed stream lost exact cleanup proof", mode, cleanupErr)
	}
	foreign := binding
	foreign.Fence = "foreign-private-fence"
	if _, cleanupErr := repository.ResolveManagedKubernetesCleanup(ctx, broker, foreign, origin.StreamRef); !errors.Is(cleanupErr, errs.ErrForbidden) {
		t.Fatal("foreign binding gained cleanup proof", cleanupErr)
	}
	result, err := managedkubernetes.BuildStreamEffectResult(terminal)
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.effect.complete"
	receipt, err := repository.CompleteManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesCompleteEffect{EffectRef: origin.EffectRef, EffectGeneration: origin.EffectGeneration, ChildInputDigest: child.InputDigest, StreamRef: origin.StreamRef, Outcome: enum.ManagedKubernetesEffectOutcome(result.Outcome), ResultJSON: result.ResultJSON, ResultDigest: result.ResultDigest, ReasonCode: result.ReasonCode})
	if err != nil || !receipt.Applied || receipt.State != "UNKNOWN" {
		t.Fatal("closed prepared stream lost its unknown terminal result", captured, err)
	}
}
