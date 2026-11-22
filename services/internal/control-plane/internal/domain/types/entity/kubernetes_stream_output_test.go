package entity

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestKubernetesStreamOutputTerminalEvidence distinguishes a confirmed joined end from loss and unjoined native completion.
// TestKubernetesStreamOutputTerminalEvidence отличает подтверждённый joined end от потерь и unjoined native completion.
func TestKubernetesStreamOutputTerminalEvidence(t *testing.T) {
	v := KubernetesStreamOutput{InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", Generation: 1, Status: "ENDED", AfterSequence: 4097, OutputWatermark: 4097, AuthorizedUntil: time.Now().Add(time.Second), NativeTerminalKnown: true, TransportJoined: true}
	if !v.Valid() {
		t.Fatal("confirmed end rejected")
	}
	for _, name := range []string{"unknown_native", "unjoined", "loss", "unread_tail", "overflow", "unknown_status"} {
		t.Run(name, func(t *testing.T) {
			x := v
			switch name {
			case "unknown_native":
				x.NativeTerminalKnown = false
			case "unjoined":
				x.TransportJoined = false
			case "loss":
				x.DroppedBytes = 1
			case "unread_tail":
				x.OutputWatermark++
			case "overflow":
				x.OutputWatermark = managedkubernetes.MaximumFrameSequence + 1
			case "unknown_status":
				x.Status = "SUCCESS"
			}
			if x.Valid() {
				t.Fatal("unproven terminal accepted")
			}
		})
	}
	v.Status = "INCOMPLETE"
	v.TransportJoined = false
	v.DroppedBytes = 3
	v.OutputWatermark++
	if !v.Valid() {
		t.Fatal("honest partial result rejected")
	}
}
