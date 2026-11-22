package grpc

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestKubernetesOutputReceiptRejectsMixedCredit preserves exact lineage and metadata-only non-ACCEPTED states.
// Historical accepted deadlines remain unchanged; no mapper may renew a lost acknowledgement.
// TestKubernetesOutputReceiptRejectsMixedCredit сохраняет exact lineage и metadata-only nonACCEPTED states.
// Historical accepted deadline не меняется; mapper не продлевает lost acknowledgement.
func TestKubernetesOutputReceiptRejectsMixedCredit(t *testing.T) {
	binding := value.ManagedKubernetesBinding{InvocationRef: "inv_original", WorkloadInstance: "gateway-original", LeaseRef: "lea_original", Fence: "opaque-original-fence", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	lookup := command.ManagedKubernetesOutputFrameLookup{StreamRef: "kst_original", Sequence: 1, DeliveryOrdinal: 2}
	receipt := entity.ManagedKubernetesOutputFrameReceipt{Ref: "kof_original", InvocationRef: binding.InvocationRef, StreamRef: lookup.StreamRef, Generation: 1, Sequence: 1, Channel: "STDOUT", PayloadDigest: strings.Repeat("b", 64), ByteCount: 4, DeliveryOrdinal: 2, State: enum.KubernetesOutputAccepted, AuthorizedUntil: time.Unix(1, 0), BindingDigest: digest}
	if mapped := managedKubernetesOutputReceiptToProto(receipt, binding, lookup); mapped == nil || !mapped.AuthorizedUntil.AsTime().Equal(receipt.AuthorizedUntil) {
		t.Fatal("accepted original credit lost")
	}
	for _, mutate := range []func(*entity.ManagedKubernetesOutputFrameReceipt){func(r *entity.ManagedKubernetesOutputFrameReceipt) { r.DeliveryOrdinal++ }, func(r *entity.ManagedKubernetesOutputFrameReceipt) { r.Generation++ }, func(r *entity.ManagedKubernetesOutputFrameReceipt) { r.State = enum.KubernetesOutputStaged }, func(r *entity.ManagedKubernetesOutputFrameReceipt) { r.AuthorizedUntil = time.Time{} }} {
		changed := receipt
		mutate(&changed)
		if managedKubernetesOutputReceiptToProto(changed, binding, lookup) != nil {
			t.Fatal("mixed output credit accepted")
		}
	}
	receipt.State = enum.KubernetesOutputUnknown
	receipt.AuthorizedUntil = time.Time{}
	if managedKubernetesOutputReceiptToProto(receipt, binding, lookup) == nil {
		t.Fatal("metadata-only uncertainty rejected")
	}
}
