package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestKubernetesCaptureLossClosesOuterSuccess preserves a loss marker independently of a native
// completed/exit0 receipt; a refused connection frame must not silently disappear from the result.
// TestKubernetesCaptureLossClosesOuterSuccess сохраняет loss marker отдельно от native completed/exit0
// receipt; отклонённый connection frame не должен молча исчезать из результата.
func TestKubernetesCaptureLossClosesOuterSuccess(t *testing.T) {
	zero := int32(0)
	terminal := managedkubernetes.TerminalReceipt{Origin: managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "kef_original_stream", EffectGeneration: 1, StreamRef: "kst_original_stream", StreamGeneration: 1, Operation: "kubernetes.exec", TargetRef: "target-original", TargetRevision: 1}, HandshakeDigest: strings.Repeat("d", 64), Protocol: string(managedkubernetes.WebSocketV5), TransportJoined: true, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", ExitCode: &zero, ReasonCode: "REMOTE_EXIT"}
	built, err := managedkubernetes.BuildStreamEffectResult(terminal)
	if err != nil {
		t.Fatal(err)
	}
	child := kubernetesInvocationChildResult{ordinal: 0, operation: terminal.Origin.Operation, state: built.Outcome, outcome: built.Outcome, result: string(built.ResultJSON), digest: built.ResultDigest, reason: built.ReasonCode, applied: true, current: true, streamRef: terminal.Origin.StreamRef, streamState: enum.KubernetesStreamCompleted, terminal: &terminal, capturePartial: true}
	state, result, err := aggregateKubernetesInvocationResults([]kubernetesInvocationChildResult{child})
	if err != nil || state != enum.KubernetesInvocationUnknown || result != string(built.ResultJSON) {
		t.Fatal("capture loss promoted outer success", state, err)
	}
}
