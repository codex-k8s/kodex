package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestKubernetesInvocationStreamAggregateDoesNotPromoteRemoteExit binds outer success to typed local
// completion/join, preserving known remote truth on LOST streams with zero or partial output.
// TestKubernetesInvocationStreamAggregateDoesNotPromoteRemoteExit связывает outer success с typed local
// completion/join, сохраняя known remote truth LOST streams с zero либо partial output.
func TestKubernetesInvocationStreamAggregateDoesNotPromoteRemoteExit(t *testing.T) {
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "kef_stream_original", EffectGeneration: 1, StreamRef: "kst_stream_original", StreamGeneration: 1, Operation: "kubernetes.exec", TargetRef: "kbt_original", TargetRevision: 1}
	zero := int32(0)
	for _, test := range []struct {
		name, state, remote, reason string
		joined, hasOutput           bool
		exit                        *int32
		want                        enum.ManagedKubernetesInvocationState
		wantErr                     error
	}{
		{"zero-output lost exit0", "LOST", "SUCCEEDED", "TRANSPORT_LOST", true, false, &zero, enum.KubernetesInvocationUnknown, nil},
		{"partial-output lost exit0", "LOST", "SUCCEEDED", "OUTPUT_LOST", true, true, &zero, enum.KubernetesInvocationUnknown, nil},
		{"unknown local join", "LOST", "UNKNOWN", "TRANSPORT_LOST", false, false, nil, enum.KubernetesInvocationUnknown, nil},
		{"normal zero-output completion", "COMPLETED", "SUCCEEDED", "REMOTE_EXIT", true, false, &zero, enum.KubernetesInvocationSucceeded, nil},
		{"complete output without artifact", "COMPLETED", "SUCCEEDED", "REMOTE_EXIT", true, true, &zero, "", errs.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			terminal := managedkubernetes.TerminalReceipt{Origin: origin, HandshakeDigest: strings.Repeat("d", 64), Protocol: string(managedkubernetes.WebSocketV5), TransportJoined: test.joined, LocalState: test.state, RemoteOutcome: test.remote, ExitCode: test.exit, ReasonCode: test.reason}
			built, err := managedkubernetes.BuildStreamEffectResult(terminal)
			if err != nil {
				t.Fatal("construct typed native fact", err)
			}
			child := kubernetesInvocationChildResult{ordinal: 0, operation: origin.Operation, state: built.Outcome, outcome: built.Outcome, result: string(built.ResultJSON), digest: built.ResultDigest, reason: built.ReasonCode, applied: true, current: true, hasOutput: test.hasOutput, streamRef: origin.StreamRef, streamState: enum.ManagedKubernetesStreamState(test.state), terminal: &terminal}
			state, raw, err := aggregateKubernetesInvocationResults([]kubernetesInvocationChildResult{child})
			if state != test.want || !errors.Is(err, test.wantErr) {
				t.Fatal("outer state promoted remote truth", state, err)
			}
			if err == nil && raw != string(built.ResultJSON) {
				t.Fatal("native remote/local truth rewritten")
			}
		})
	}
}
