package grpc

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestKubernetesInvocationReceiptCannotPromoteUnrecordedClosure preserves exact original claim pins.
// Metadata-only noPlan and cancelled closure cannot become business success or result delivery.
// TestKubernetesInvocationReceiptCannotPromoteUnrecordedClosure сохраняет exact original claim pins.
// Metadata-only noPlan и cancelled closure не становятся business success либо result delivery.
func TestKubernetesInvocationReceiptCannotPromoteUnrecordedClosure(t *testing.T) {
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "worker", InvocationRef: "inv_receipt", LeaseRef: "lea_receipt", Fence: "synthetic-private-fence", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	digest, _ := managedkubernetes.BindingDigest(binding)
	receipt := entity.ManagedKubernetesInvocationReceipt{InvocationRef: binding.InvocationRef, Generation: 1, InputDigest: binding.InputDigest, BindingDigest: digest, State: enum.KubernetesInvocationCancelled, CompletionPendingReason: enum.KubernetesCompletionPendingNone}
	if managedKubernetesInvocationReceiptToProto(receipt, binding) == nil {
		t.Fatal("unrecorded closure lost metadata readback")
	}
	receipt.State = enum.KubernetesInvocationSucceeded
	if managedKubernetesInvocationReceiptToProto(receipt, binding) != nil {
		t.Fatal("unrecorded closure promoted to success")
	}
	receipt.State = enum.KubernetesInvocationFailed
	receipt.CompletionRecorded = true
	receipt.ReceiptRef = "erc_receipt"
	receipt.ResultDigest = strings.Repeat("b", 64)
	if managedKubernetesInvocationReceiptToProto(receipt, binding) == nil {
		t.Fatal("recorded noPlan receipt lost exact binding")
	}
	for _, change := range []func(*entity.ManagedKubernetesInvocationReceipt){
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.BindingDigest = strings.Repeat("c", 64) },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.InvocationRef = "inv_foreign" },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.InputDigest = strings.Repeat("c", 64) },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.Generation++ },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.State = enum.KubernetesInvocationRunning },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.ReceiptRef = "" },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.PlanRef = "kpl_partial" },
		func(r *entity.ManagedKubernetesInvocationReceipt) { r.SnapshotDigest = strings.Repeat("d", 64) },
	} {
		changed := receipt
		change(&changed)
		if managedKubernetesInvocationReceiptToProto(changed, binding) != nil {
			t.Fatal("foreign or mixed receipt became authoritative metadata")
		}
	}
	receipt.State = enum.KubernetesInvocationSucceeded
	receipt.PlanRef = "kpl_receipt"
	receipt.PlanRevision = 1
	receipt.PlanDigest = strings.Repeat("c", 64)
	receipt.SnapshotDigest = strings.Repeat("d", 64)
	if managedKubernetesInvocationReceiptToProto(receipt, binding) == nil {
		t.Fatal("complete original plan tuple was rejected")
	}
}
