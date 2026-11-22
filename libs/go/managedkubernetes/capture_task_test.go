package managedkubernetes

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestCaptureTaskBindingKeepsFencePrivateAndPinsSource checks redaction and immutable task context
// commitments independently of owner authority, which still requires the persisted task grant.
// TestCaptureTaskBindingKeepsFencePrivateAndPinsSource проверяет redaction и immutable task context
// commitments отдельно от owner authority: persisted task grant остаётся обязательным.
func TestCaptureTaskBindingKeepsFencePrivateAndPinsSource(t *testing.T) {
	original := CaptureTaskBinding{CaptureRef: "koc_original_capture", LeaseRef: "kca_original_attempt", Fence: strings.Repeat("private-fixture-", 4), Generation: 1, WorkloadInstance: "capture-worker", SnapshotDigest: strings.Repeat("a", 64)}
	digest, err := original.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), original.Fence) || strings.Contains(fmt.Sprintf("%v %+v %#v", original, original, original), original.Fence) {
		t.Fatal("capture fence escaped redaction")
	}
	for _, change := range []func(*CaptureTaskBinding){
		func(b *CaptureTaskBinding) { b.Generation++ },
		func(b *CaptureTaskBinding) { b.CaptureRef = "koc_successor_capture" },
		func(b *CaptureTaskBinding) { b.LeaseRef = "kca_successor_attempt" },
		func(b *CaptureTaskBinding) { b.WorkloadInstance = "capture-other" },
		func(b *CaptureTaskBinding) { b.SnapshotDigest = strings.Repeat("b", 64) },
		func(b *CaptureTaskBinding) { b.Fence += "different" },
	} {
		changed := original
		change(&changed)
		other, err := changed.Digest()
		if err != nil || other == digest {
			t.Fatal("capture binding omitted an incarnation pin", err)
		}
	}
	original.Fence = ""
	if original.Validate() == nil {
		t.Fatal("empty capture task fence accepted")
	}
	if ValidCaptureReason("PRIVATE_STORAGE_SENTINEL") {
		t.Fatal("raw diagnostic became capture reason")
	}
}
