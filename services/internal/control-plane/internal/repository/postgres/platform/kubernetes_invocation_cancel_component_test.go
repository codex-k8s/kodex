package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertKubernetesWorkerCancellation checks exact shutdown closure for noPlan, a pending child Gate
// and an active owner-created stream. Synthetic handshake evidence is not a native TLS/join claim.
// assertKubernetesWorkerCancellation проверяет exact shutdown closure для noPlan, pending child Gate
// и active owner-created stream. Synthetic handshake evidence не доказывает native TLS/join.
func assertKubernetesWorkerCancellation(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, gateway, broker value.Principal, claim func(string, string, map[string]any) value.ManagedKubernetesBinding, writeInput map[string]any, resource managedkubernetes.ChildCandidate) {
	t.Helper()
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName {
			gateway.Permission = operation
		}
	}
	for _, mode := range []string{"NO_PLAN", "GATE", "ACTIVE_STREAM"} {
		operation := "kubernetes.replace"
		input := writeInput
		candidate := resource
		if mode != "GATE" {
			operation = "kubernetes.logs"
			input = map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "options": map[string]any{"container": "main", "timeoutSeconds": 120}}
			candidate.Resource = managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17", Namespaced: true}
		}
		binding := claim(operation, "kube-worker-stop-"+mode, input)
		foreign := binding
		foreign.WorkloadInstance = "foreign-stop-worker"
		if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, foreign); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("foreign worker stopped execution", mode, err)
		}
		var streamRef string
		var snapshotDigest string
		if mode != "NO_PLAN" {
			broker.Permission = "platform.runtime.kubernetes.resolve"
			snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
			if err != nil {
				t.Fatal(err)
			}
			snapshotDigest = snapshot.Digest
			proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
			if err != nil {
				t.Fatal(err)
			}
			broker.Permission = "platform.runtime.kubernetes.children.prepare"
			plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
			if err != nil {
				t.Fatal(err)
			}
			child := plan.Children[0]
			broker.Permission = "platform.runtime.kubernetes.effect.begin"
			admission, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest})
			if mode == "GATE" {
				var waiting *platformservice.KubernetesApprovalRequiredError
				if !errors.As(err, &waiting) {
					t.Fatal("pending worker Gate missing", err)
				}
			} else {
				if err != nil || admission.Stream == nil {
					t.Fatal("active worker stream missing", err)
				}
				streamRef = admission.Stream.Ref
				digest, _ := managedkubernetes.BindingDigest(binding)
				origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: digest, ChildInputDigest: child.InputDigest, EffectRef: admission.Ref, EffectGeneration: admission.Generation, StreamRef: streamRef, StreamGeneration: binding.Generation, Operation: operation, TargetRef: snapshot.Target.Ref, TargetRevision: snapshot.Target.Revision}
				handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: origin.TargetRef, TransportGeneration: origin.TargetRevision, Protocol: managedkubernetes.HTTPLogsV1, HTTPStatus: 200}
				hash, _ := handshake.Digest()
				broker.Permission = "platform.runtime.kubernetes.stream.open"
				if _, err = repository.OpenManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesOpenStream{StreamRef: streamRef, EffectRef: admission.Ref, EffectGeneration: admission.Generation, HandshakeReceiptDigest: hash, HandshakeReceipt: handshake}); err != nil {
					t.Fatal(err)
				}
				broker.Permission = "platform.runtime.kubernetes.frame.begin"
				if _, err = repository.BeginManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesBeginFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameOutput, NativeAuthorizedUntil: time.Now().Add(4 * time.Second), Sequence: 1, Channel: "STDOUT", PayloadDigest: strings.Repeat("e", 64), ByteCount: 4}); err != nil {
					t.Fatal(err)
				}
			}
		}
		var sibling value.ManagedKubernetesBinding
		if mode == "GATE" {
			sibling = claim("kubernetes.replace", "kube-worker-stop-GATE-SIBLING", writeInput)
			broker.Permission = "platform.runtime.kubernetes.resolve"
			snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, sibling)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{resource})
			if err != nil {
				t.Fatal(err)
			}
			broker.Permission = "platform.runtime.kubernetes.children.prepare"
			plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, sibling, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{resource}})
			if err != nil {
				t.Fatal(err)
			}
			broker.Permission = "platform.runtime.kubernetes.effect.begin"
			assertKubernetesGateRootOverlap(t, ctx, repository, broker, binding, sibling, command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: plan.Children[0].Ref, ChildInputDigest: plan.Children[0].InputDigest})
		}
		receipt, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding)
		expected := enum.KubernetesInvocationCancelled
		if mode == "ACTIVE_STREAM" {
			expected = enum.KubernetesInvocationUnknown
		}
		if err != nil || receipt.State != expected || receipt.CompletionRecorded {
			t.Fatal("shutdown invented outcome or kept execution live", mode, receipt, err)
		}
		repeated, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding)
		if err != nil || repeated != receipt {
			t.Fatal("lost stop ACK changed exact terminal metadata", mode, err)
		}
		expectedRootState := "RUNNING"
		if sibling.InvocationRef != "" {
			expectedRootState = "WAITING_HUMAN"
		}
		var closed bool
		if repository.pool.QueryRow(ctx, `SELECT invocation.lease_ref IS NULL AND invocation.effect_fence_digest IS NULL AND invocation.workload_instance IS NULL
   AND NOT EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=invocation.id AND gate.state='OPEN')
   AND root.state=$2 FROM control_plane.integration_invocations invocation JOIN control_plane.runs root ON root.id=invocation.run_id WHERE invocation.ref=$1`, binding.InvocationRef, expectedRootState).Scan(&closed) != nil || !closed {
			t.Fatal("shutdown left Gate/lease live or cancelled whole Run", mode)
		}
		if sibling.InvocationRef != "" {
			var siblingOpen bool
			if repository.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control_plane.owner_gates gate JOIN control_plane.integration_invocations invocation ON invocation.id=gate.integration_invocation_id WHERE invocation.ref=$1 AND gate.state='OPEN')", sibling.InvocationRef).Scan(&siblingOpen) != nil || !siblingOpen {
				t.Fatal("cancel consumed independent sibling Gate")
			}
			if _, err = service.CancelManagedKubernetesInvocation(ctx, gateway, sibling); err != nil {
				t.Fatal(err)
			}
			var resumed bool
			if repository.pool.QueryRow(ctx, "SELECT root.state='RUNNING' FROM control_plane.integration_invocations invocation JOIN control_plane.runs execution ON execution.id=invocation.run_id JOIN control_plane.runs root ON root.id=execution.root_run_id WHERE invocation.ref=$1", sibling.InvocationRef).Scan(&resumed) != nil || !resumed {
				t.Fatal("last Gate closure left root waiting without a Gate")
			}
		}
		if streamRef != "" {
			broker.Permission = "platform.runtime.kubernetes.resolve"
			cleanup, err := repository.ResolveManagedKubernetesCleanup(ctx, broker, binding, streamRef)
			if err != nil || !cleanup.ExecutionClosed || cleanup.SnapshotDigest != snapshotDigest {
				t.Fatal("shutdown lacked exact broker cleanup proof", err)
			}
			broker.Permission = "platform.runtime.kubernetes.frame.begin"
			if _, err = repository.BeginManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesBeginFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameOutput, NativeAuthorizedUntil: time.Now().Add(4 * time.Second), Sequence: 2, Channel: "STDOUT", PayloadDigest: strings.Repeat("f", 64), ByteCount: 4}); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("shutdown left frame authority", err)
			}
		}
	}
}
