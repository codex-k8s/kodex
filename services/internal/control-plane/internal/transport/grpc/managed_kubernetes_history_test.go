package grpc

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestManagedKubernetesCleanupRequiresExactOriginalClaim prevents closure proof reuse across claims.
// Active stream metadata is valid only when the owner separately proves execution closure.
// TestManagedKubernetesCleanupRequiresExactOriginalClaim запрещает reuse closure proof между claims.
// Active stream metadata допустима только с отдельным owner proof закрытия execution.
func TestManagedKubernetesCleanupRequiresExactOriginalClaim(t *testing.T) {
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "worker", InvocationRef: "inv_cleanup", LeaseRef: "lea_cleanup", Fence: "synthetic-private-fence", Generation: 3, InputDigest: strings.Repeat("a", 64)}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal("invalid fixture binding")
	}
	proof := entity.ManagedKubernetesCleanupReceipt{InvocationRef: binding.InvocationRef, StreamRef: "kst_cleanup", Generation: binding.Generation, BindingDigest: digest, SnapshotDigest: strings.Repeat("b", 64), State: enum.KubernetesStreamActive, ExecutionClosed: true}
	if managedKubernetesCleanupToProto(proof, binding, proof.StreamRef) == nil {
		t.Fatal("closed execution lost cleanup metadata")
	}
	for _, change := range []func(*entity.ManagedKubernetesCleanupReceipt){
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.InvocationRef = "inv_foreign" },
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.StreamRef = "kst_foreign" },
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.Generation++ },
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.BindingDigest = strings.Repeat("c", 64) },
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.SnapshotDigest = "" },
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.ExecutionClosed = false },
		func(p *entity.ManagedKubernetesCleanupReceipt) { p.State = "FUTURE" },
	} {
		changed := proof
		change(&changed)
		if managedKubernetesCleanupToProto(changed, binding, proof.StreamRef) != nil {
			t.Fatal("unbound closure became cleanup proof")
		}
	}
	changed := binding
	changed.Fence = "foreign-private-fence"
	if managedKubernetesCleanupToProto(proof, changed, proof.StreamRef) != nil {
		t.Fatal("closure crossed an original fence")
	}
}

// TestManagedKubernetesHistoryPreservesUnreportedAndValidatedEvidence grants no fresh authority.
// It retains expired deadlines and refuses changed child, stream or terminal lineage.
// TestManagedKubernetesHistoryPreservesUnreportedAndValidatedEvidence не выдаёт fresh authority.
// Сохраняет expired deadlines и отклоняет changed child, stream либо terminal lineage.
func TestManagedKubernetesHistoryPreservesUnreportedAndValidatedEvidence(t *testing.T) {
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "worker", InvocationRef: "inv_history", LeaseRef: "lea_history", Fence: "synthetic-private-fence", Generation: 2, InputDigest: strings.Repeat("a", 64)}
	digest, _ := managedkubernetes.BindingDigest(binding)
	history := entity.ManagedKubernetesStreamHistory{Ref: "kst_history", Generation: 2, State: enum.KubernetesStreamPrepared, HardDeadline: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	wire := managedKubernetesHistoryToProto(&history)
	if wire == nil || wire.HandshakeReceipt != nil || wire.TerminalReceipt != nil || !wire.HardDeadline.AsTime().Equal(history.HardDeadline) {
		t.Fatal("unreported handshake invented evidence or refreshed deadline")
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("b", 64), BindingDigest: digest, ChildInputDigest: strings.Repeat("c", 64), EffectRef: "kef_history", EffectGeneration: 1, StreamRef: history.Ref, StreamGeneration: history.Generation, Operation: "kubernetes.logs", TargetRef: "target_history", TargetRevision: 1}
	handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: origin.TargetRef, TransportGeneration: 1, Protocol: managedkubernetes.HTTPLogsV1, HTTPStatus: 200}
	hash, err := handshake.Digest()
	if err != nil {
		t.Fatal("invalid handshake fixture")
	}
	terminal := managedkubernetes.TerminalReceipt{Origin: origin, HandshakeDigest: hash, Protocol: handshake.Protocol, TransportJoined: true, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", CleanEOF: true, ReasonCode: "CLEAN_EOF"}
	history.Handshake = &handshake
	history.Terminal = &terminal
	history.State = enum.KubernetesStreamCompleted
	if managedKubernetesHistoryToProto(&history) == nil || !managedKubernetesHistoryMatches(&history, origin.EffectRef, 1, origin.ChildInputDigest, binding) {
		t.Fatal("valid historical evidence lost original lineage")
	}
	if managedKubernetesHistoryMatches(&history, "kef_foreign", 1, origin.ChildInputDigest, binding) {
		t.Fatal("foreign child accepted historical evidence")
	}
	terminal.Origin.TargetRevision++
	if managedKubernetesHistoryToProto(&history) != nil {
		t.Fatal("changed terminal origin retained old handshake")
	}
	terminal.Origin = origin
	history.State = enum.KubernetesStreamLost
	if managedKubernetesHistoryToProto(&history) == nil {
		t.Fatal("owner closure erased an independently proven native terminal outcome")
	}
	history.State = enum.KubernetesStreamCompleted
	terminal.LocalState = "LOST"
	terminal.ReasonCode = "OUTPUT_LOST"
	if managedKubernetesHistoryToProto(&history) != nil {
		t.Fatal("owner metadata promoted incomplete native delivery to completed")
	}
	history.State = enum.KubernetesStreamActive
	if managedKubernetesHistoryToProto(&history) != nil {
		t.Fatal("active history accepted terminal evidence")
	}

}
