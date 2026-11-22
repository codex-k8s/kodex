package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestInputFrameDeliveryRejectsForeignOrReplayedShape binds private bytes to the exact original
// claim and a bounded source delivery; neither hashes nor a valid public channel mint authority.
// TestInputFrameDeliveryRejectsForeignOrReplayedShape связывает private bytes с exact original
// claim и bounded source delivery; hash/public channel не назначают authority.
func TestInputFrameDeliveryRejectsForeignOrReplayedShape(t *testing.T) {
	now := time.Unix(1800000000, 0)
	binding := Binding{InvocationRef: "inv_original", WorkloadInstance: "gateway-original", LeaseRef: "lea_original", Fence: "original-private-fence", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	digest, err := BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("private-input-sentinel")
	sum := sha256.Sum256(payload)
	base := InputFrameDelivery{Ref: "kif_original_source", InvocationRef: binding.InvocationRef, StreamRef: "kst_original", Generation: 1, Sequence: 1, Channel: "STDIN", Payload: payload, ByteCount: uint32(len(payload)), PayloadDigest: hex.EncodeToString(sum[:]), BindingDigest: digest, DeliveryExpiresAt: now.Add(5 * time.Second)}
	if err := base.Validate(binding, base.StreamRef, now); err != nil {
		t.Fatal("valid first input delivery rejected", err)
	}
	for _, sequence := range []uint64{4097, 1 << 20, MaximumFrameSequence} {
		candidate := base
		candidate.Sequence = sequence
		if candidate.Validate(binding, base.StreamRef, now) != nil {
			t.Fatal("representable input sequence rejected", sequence)
		}
	}
	for _, change := range []func(*InputFrameDelivery){
		func(f *InputFrameDelivery) { f.Ref = "kif_a" }, func(f *InputFrameDelivery) { f.InvocationRef += "_foreign" }, func(f *InputFrameDelivery) { f.StreamRef += "_foreign" }, func(f *InputFrameDelivery) { f.Generation++ }, func(f *InputFrameDelivery) { f.Sequence = MaximumFrameSequence + 1 }, func(f *InputFrameDelivery) { f.BindingDigest = strings.Repeat("b", 64) }, func(f *InputFrameDelivery) { f.Payload = []byte("changed") }, func(f *InputFrameDelivery) { f.ByteCount++ }, func(f *InputFrameDelivery) { f.Channel = "CONTROL" }, func(f *InputFrameDelivery) { f.Channel = "TERMINAL_RESIZE" }, func(f *InputFrameDelivery) { f.EndOfInput = true }, func(f *InputFrameDelivery) { f.DeliveryExpiresAt = now }, func(f *InputFrameDelivery) {
			f.DeliveryExpiresAt = now.Add(MaximumInputDeliveryLifetime + time.Nanosecond)
		},
	} {
		candidate := base
		change(&candidate)
		if candidate.Validate(binding, base.StreamRef, now) == nil {
			t.Fatal("foreign/changed input delivery accepted")
		}
	}
	encoded, err := json.Marshal(base)
	if err != nil || strings.Contains(string(encoded), string(payload)) || strings.Contains(fmt.Sprintf("%v %#v", base, base), string(payload)) {
		t.Fatal("private input escaped diagnostic boundary")
	}
	base.Channel = "FILE_CONTENT"
	if base.Validate(binding, base.StreamRef, now) != nil {
		t.Fatal("structurally valid private artifact chunk rejected")
	}
	base.Payload = nil
	base.ByteCount = 0
	base.EndOfInput = true
	empty := sha256.Sum256(nil)
	base.PayloadDigest = hex.EncodeToString(empty[:])
	if base.Validate(binding, base.StreamRef, now) != nil {
		t.Fatal("private artifact EOF rejected")
	}
}
