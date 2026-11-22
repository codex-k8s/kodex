package command

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"testing"
)

// TestKubernetesOutputFrameInputBindsExactBytes rejects changed digests, sizes, input channels and delivery ordinals.
// TestKubernetesOutputFrameInputBindsExactBytes отвергает changed digest/size/input channel/delivery ordinal.
func TestKubernetesOutputFrameInputBindsExactBytes(t *testing.T) {
	payload := []byte("synthetic output")
	digest := sha256.Sum256(payload)
	original := ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: ManagedKubernetesOutputFrameLookup{StreamRef: "kst_original", Sequence: 1, DeliveryOrdinal: 1}, Channel: "STDOUT", ByteCount: uint32(len(payload)), PayloadDigest: hex.EncodeToString(digest[:]), Payload: payload}
	if !original.Valid() {
		t.Fatal("valid owned output rejected")
	}
	for _, sequence := range []uint64{4097, 1 << 20, managedkubernetes.MaximumFrameSequence} {
		candidate := original
		candidate.Sequence = sequence
		if !candidate.Valid() {
			t.Fatal("representable output sequence rejected", sequence)
		}
	}
	for _, mutate := range []func(*ManagedKubernetesAcceptOutputFrame){
		func(input *ManagedKubernetesAcceptOutputFrame) { input.ByteCount++ },
		func(input *ManagedKubernetesAcceptOutputFrame) { input.DeliveryOrdinal = 0 },
		func(input *ManagedKubernetesAcceptOutputFrame) { input.DeliveryOrdinal = 5 },
		func(input *ManagedKubernetesAcceptOutputFrame) {
			input.Sequence = managedkubernetes.MaximumFrameSequence + 1
		},
		func(input *ManagedKubernetesAcceptOutputFrame) { input.Channel = "STDIN" },
		func(input *ManagedKubernetesAcceptOutputFrame) { input.Payload = []byte("different output") },
	} {
		changed := original
		mutate(&changed)
		if changed.Valid() {
			t.Fatal("invalid output crossed structural admission")
		}
	}
}
