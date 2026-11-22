package httptransport

import (
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// captureOriginWireFixture carries a partial confirmed transcript with a known remote zero exit.
// captureOriginWireFixture передаёт partial confirmed transcript с known remote zero exit.
func captureOriginWireFixture() *cp.KubernetesArtifactCaptureOrigin {
	zero := int32(0)
	return &cp.KubernetesArtifactCaptureOrigin{InvocationRef: "inv_fixture123", StreamRef: "kst_fixture123", StreamGeneration: 1, CaptureGeneration: 2, Operation: "kubernetes.exec", Channel: "STDOUT", Kind: cp.KubernetesArtifactCaptureKind_KUBERNETES_ARTIFACT_CAPTURE_KIND_PARTIAL, AcknowledgedPrefix: 9007199254740993, OutputWatermark: 9007199254740994, NativeTerminalKnown: true, TransportJoined: true, OwnerState: "LOST", LocalState: "LOST", RemoteOutcome: "SUCCEEDED", ExitCode: &zero, Resource: &cp.KubernetesOwnerGateResource{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", Uid: "30000000-0000-4000-8000-000000000001", ResourceVersion: "17", Namespaced: true}, TargetRef: "operator/target", TargetRevision: 1, ConnectionRef: "int_fixture123", SourceDigest: strings.Repeat("a", 64), ReasonCode: "OUTPUT_LOST", SealedAt: timestamppb.New(time.Now())}
}

// TestKubernetesCapturePublicProjectionPreservesCoverageAndExactOrdinal exercises the actual
// generic protobuf JSON path, including required zero defaults and safe partial remote outcomes.
// TestKubernetesCapturePublicProjectionPreservesCoverageAndExactOrdinal проверяет actual generic
// protobuf JSON path, required zero defaults и safe partial remote outcomes.
func TestKubernetesCapturePublicProjectionPreservesCoverageAndExactOrdinal(t *testing.T) {
	value := captureOriginWireFixture()
	mapped, err := messageMap(value)
	if err != nil || mapped["kind"] != "PARTIAL" || mapped["acknowledgedPrefix"] != "9007199254740993" || mapped["droppedBytes"] != "0" || mapped["exitCode"] != float64(0) {
		t.Fatal("capture projection changed source truth", err)
	}
	for _, change := range []func(*cp.KubernetesArtifactCaptureOrigin){
		func(v *cp.KubernetesArtifactCaptureOrigin) {
			v.Kind = cp.KubernetesArtifactCaptureKind_KUBERNETES_ARTIFACT_CAPTURE_KIND_FULL
		},
		func(v *cp.KubernetesArtifactCaptureOrigin) { v.NativeTerminalKnown = false },
		func(v *cp.KubernetesArtifactCaptureOrigin) { v.RemoteOutcome = "UNKNOWN" },
		func(v *cp.KubernetesArtifactCaptureOrigin) { v.Kind = cp.KubernetesArtifactCaptureKind(999) },
		func(v *cp.KubernetesArtifactCaptureOrigin) { v.OutputWatermark = 18446744073709551615 },
		func(v *cp.KubernetesArtifactCaptureOrigin) { v.Resource.Uid = "" },
	} {
		bad := proto.Clone(value).(*cp.KubernetesArtifactCaptureOrigin)
		change(bad)
		if mapped, err := messageMap(bad); err == nil || mapped != nil {
			t.Fatal("invalid capture projection was silently published")
		}
	}
}
