package managedkubernetes

import (
	"math"
	"strings"
	"testing"
	"time"
)

// captureOriginFixture retains a partial transcript despite a confirmed remote zero exit.
// captureOriginFixture сохраняет partial transcript даже при confirmed remote zero exit.
func captureOriginFixture() ArtifactCaptureOrigin {
	zero := int32(0)
	return ArtifactCaptureOrigin{InvocationRef: "inv_fixture123", StreamRef: "kst_fixture123", StreamGeneration: 2, CaptureGeneration: 3, Operation: "kubernetes.exec", Channel: "STDOUT", Kind: "PARTIAL", AcknowledgedPrefix: 1, OutputWatermark: 2, NativeTerminalKnown: true, TransportJoined: true, OwnerState: "LOST", LocalState: "LOST", RemoteOutcome: "SUCCEEDED", ExitCode: &zero, Resource: ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "review", Name: "web", UID: "0b3ec20a-2cf3-4656-a89b-934952b05e34", ResourceVersion: "17"}, TargetRef: "target/review", TargetRevision: 1, ConnectionRef: "int_fixture123", SourceDigest: strings.Repeat("a", 64), ReasonCode: "OUTPUT_LOST", SealedAt: time.Now().UTC()}
}

// TestCaptureOriginSeparatesCoverageFromRemoteOutcome refuses forged full, join and unknown proof.
// TestCaptureOriginSeparatesCoverageFromRemoteOutcome отклоняет forged full/join/unknown proof.
func TestCaptureOriginSeparatesCoverageFromRemoteOutcome(t *testing.T) {
	base := captureOriginFixture()
	if base.Validate() != nil {
		t.Fatal("partial remote-success provenance was rejected")
	}
	for _, change := range []func(*ArtifactCaptureOrigin){
		func(v *ArtifactCaptureOrigin) { v.Kind = "FULL" },
		func(v *ArtifactCaptureOrigin) { v.NativeTerminalKnown = false },
		func(v *ArtifactCaptureOrigin) { v.OutputWatermark = math.MaxUint64 },
		func(v *ArtifactCaptureOrigin) { v.RemoteOutcome = "UNKNOWN" },
		func(v *ArtifactCaptureOrigin) { v.Channel = "PRIVATE_BODY" },
		func(v *ArtifactCaptureOrigin) { v.ReasonCode = "private upstream response" },
		func(v *ArtifactCaptureOrigin) { v.Resource.UID = "" },
	} {
		changed := base
		change(&changed)
		if changed.Validate() == nil {
			t.Fatal("forged capture evidence accepted")
		}
	}
	base.OwnerState = "COMPLETED"
	base.LocalState = "COMPLETED"
	base.Kind = "FULL"
	base.OutputWatermark = 1
	base.ReasonCode = "REMOTE_EXIT"
	if base.Validate() != nil {
		t.Fatal("full transcript did not validate")
	}
	base.Kind = "PARTIAL"
	base.NativeTerminalKnown = false
	base.TransportJoined = false
	base.LocalState = "UNKNOWN"
	base.RemoteOutcome = "UNKNOWN"
	base.ExitCode = nil
	base.ReasonCode = ""
	if base.Validate() != nil {
		t.Fatal("owner-closed prefix required fabricated native proof")
	}
}
