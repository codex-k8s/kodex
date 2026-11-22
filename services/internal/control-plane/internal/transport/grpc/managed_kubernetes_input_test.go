package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestManagedKubernetesInputWirePreservesOneOwnedDelivery rejects mixed lineage and data hidden
// behind NONE/UNKNOWN/CLOSED, while the outgoing bounded bytes do not alias the domain buffer.
// TestManagedKubernetesInputWirePreservesOneOwnedDelivery отвергает mixed lineage/data за
// NONE/UNKNOWN/CLOSED; outgoing bounded bytes не ссылаются на domain buffer.
func TestManagedKubernetesInputWirePreservesOneOwnedDelivery(t *testing.T) {
	now := time.Now()
	binding := value.ManagedKubernetesBinding{InvocationRef: "inv_original", WorkloadInstance: "gateway-original", LeaseRef: "lea_original", Fence: "original-private-fence", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	digest, _ := managedkubernetes.BindingDigest(binding)
	payload := []byte("input")
	sum := sha256.Sum256(payload)
	frame := entity.ManagedKubernetesClaimedInputFrame{Ref: "kif_original_source", InvocationRef: binding.InvocationRef, StreamRef: "kst_original", Generation: 1, Sequence: 1, Channel: "STDIN", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: 5, Payload: payload, DeliveryExpiresAt: now.Add(5 * time.Second), BindingDigest: digest}
	result := entity.ManagedKubernetesInputFrameAdmission{Disposition: enum.KubernetesInputFrameDeliver, Frame: &frame}
	wire := managedKubernetesInputAdmissionToProto(result, binding, frame.StreamRef, now)
	if wire == nil || wire.GetFrame() == nil || string(wire.Frame.Payload) != "input" {
		t.Fatal("valid private input delivery rejected")
	}
	clear(payload)
	if string(wire.Frame.Payload) != "input" {
		t.Fatal("outgoing input payload aliased cleared domain bytes")
	}
	frame.Payload = []byte("input")
	for _, mode := range []enum.ManagedKubernetesInputFrameDisposition{enum.KubernetesInputFrameNone, enum.KubernetesInputFrameUnknown, enum.KubernetesInputFrameClosed} {
		result.Disposition = mode
		if managedKubernetesInputAdmissionToProto(result, binding, frame.StreamRef, now) != nil {
			t.Fatal("non-delivery carried hidden bytes")
		}
		result.Frame = nil
		if managedKubernetesInputAdmissionToProto(result, binding, frame.StreamRef, now) == nil {
			t.Fatal("metadata-only disposition rejected")
		}
		result.Frame = &frame
	}
	result.Disposition = enum.KubernetesInputFrameDeliver
	frame.Generation++
	if managedKubernetesInputAdmissionToProto(result, binding, frame.StreamRef, now) != nil {
		t.Fatal("foreign generation input delivered")
	}
}
