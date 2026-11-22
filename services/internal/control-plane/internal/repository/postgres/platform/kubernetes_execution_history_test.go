package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestKubernetesStreamHistoryPreservesOriginalEvidence rejects corrupted or invented readback facts.
// Owner terminal narrowing retains native truth; unreported handshakes never become failed dial proof.
// TestKubernetesStreamHistoryPreservesOriginalEvidence отклоняет corrupted либо invented readback facts.
// Owner terminal narrowing сохраняет native truth; unreported handshake не становится failed dial proof.
func TestKubernetesStreamHistoryPreservesOriginalEvidence(t *testing.T) {
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "worker_history", InvocationRef: "inv_history", LeaseRef: "lea_history", Fence: "synthetic-private-fence", Generation: 2, InputDigest: strings.Repeat("a", 64)}
	record := kubernetesStreamRecord{effectRef: "kef_history", effectGeneration: 1, snapshotDigest: strings.Repeat("b", 64), inputDigest: strings.Repeat("c", 64), operation: "kubernetes.logs", targetRef: "target_history", targetRevision: 1, generation: 2, state: enum.KubernetesStreamPrepared, hardDeadline: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), remoteOutcome: enum.KubernetesEffectUnknown}
	if history, err := record.history(binding, "kst_history"); err != nil || history.Handshake != nil || history.Terminal != nil || !history.HardDeadline.Equal(record.hardDeadline) {
		t.Fatal("unreported stream invented evidence or refreshed its hard deadline", err)
	}
	handshake := managedkubernetes.HandshakeReceipt{Origin: record.origin(binding, "kst_history"), TransportRef: record.targetRef, TransportGeneration: record.targetRevision, Protocol: managedkubernetes.HTTPLogsV1, HTTPStatus: 200}
	record.handshakeDigest, _ = handshake.Digest()
	record.handshakeJSON, _ = json.Marshal(handshake)
	record.state = enum.KubernetesStreamActive
	if history, err := record.history(binding, "kst_history"); err != nil || history.Handshake == nil || history.Terminal != nil {
		t.Fatal("matching handshake was lost from historical metadata", err)
	}
	terminal := managedkubernetes.TerminalReceipt{Origin: handshake.Origin, HandshakeDigest: record.handshakeDigest, Protocol: handshake.Protocol, TransportJoined: true, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", CleanEOF: true, ReasonCode: "CLEAN_EOF"}
	record.terminalDigest, _ = terminal.Digest()
	record.terminalJSON, _ = json.Marshal(terminal)
	record.remoteOutcome = enum.KubernetesEffectSucceeded
	record.state = enum.KubernetesStreamRevoked
	if history, err := record.history(binding, "kst_history"); err != nil || history.State != enum.KubernetesStreamRevoked || history.Terminal == nil || history.Terminal.RemoteOutcome != "SUCCEEDED" {
		t.Fatal("owner revocation erased proven remote truth", err)
	}
	for _, change := range []func(*kubernetesStreamRecord){
		func(r *kubernetesStreamRecord) { r.handshakeDigest = strings.Repeat("d", 64) },
		func(r *kubernetesStreamRecord) { r.handshakeJSON = nil },
		func(r *kubernetesStreamRecord) { r.terminalDigest = strings.Repeat("d", 64) },
		func(r *kubernetesStreamRecord) { r.terminalJSON = nil },
		func(r *kubernetesStreamRecord) { r.remoteOutcome = enum.KubernetesEffectUnknown },
		func(r *kubernetesStreamRecord) { r.state = enum.KubernetesStreamActive },
		func(r *kubernetesStreamRecord) { r.generation++ },
		func(r *kubernetesStreamRecord) { r.inputDigest = strings.Repeat("d", 64) },
		func(r *kubernetesStreamRecord) { r.targetRevision++ },
		func(r *kubernetesStreamRecord) { r.handshakeJSON = []byte(`{"origin":{},"origin":{}}`) },
	} {
		changed := record
		change(&changed)
		if _, err := changed.history(binding, "kst_history"); err == nil {
			t.Fatal("corrupted stored receipt became valid historical evidence")
		}
	}
	record.state = enum.KubernetesStreamCompleted
	terminal.LocalState = "LOST"
	terminal.RemoteOutcome = "UNKNOWN"
	terminal.CleanEOF = false
	terminal.ReasonCode = "OUTPUT_LOST"
	record.remoteOutcome = enum.KubernetesEffectUnknown
	record.terminalDigest, _ = terminal.Digest()
	record.terminalJSON, _ = json.Marshal(terminal)
	if _, err := record.history(binding, "kst_history"); err == nil {
		t.Fatal("owner readback promoted unknown local delivery to completed")
	}
}
