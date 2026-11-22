package controlplane

import (
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// historyAdapterFixture provides a typed historical handshake with exact original effect and claim pins.
// historyAdapterFixture задаёт typed historical handshake с exact original effect и claim pins.
func historyAdapterFixture(t *testing.T) (*Owner, *effectClientFixture, managedkubernetes.Binding, targetrepo.EffectOpening) {
	t.Helper()
	owner, client, binding, opening, _ := operationEffectFixture()
	opening.Operation = "kubernetes.exec"
	client.beginEffect.ReconcileOnly = true
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: digest, ChildInputDigest: opening.ChildInputDigest, EffectRef: client.beginEffect.EffectRef, EffectGeneration: client.beginEffect.EffectGeneration, StreamRef: "stream_synthetic", StreamGeneration: binding.Generation, Operation: opening.Operation, TargetRef: "target_synthetic", TargetRevision: 2}
	handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: "transport_synthetic", TransportGeneration: 3, Protocol: string(managedkubernetes.WebSocketV5), HTTPStatus: 101, NegotiatedProtocol: managedkubernetes.WebSocketV5.WireProtocol(), PeerSPKISHA256: strings.Repeat("d", 64)}
	client.beginEffect.StreamHistory = &cp.ManagedKubernetesStreamHistory{StreamRef: origin.StreamRef, Generation: binding.Generation, State: cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE, HardDeadline: timestamppb.New(time.Now().Add(-time.Minute)), HandshakeReceipt: kubernetesHandshakeToProto(handshake)}
	return owner, client, binding, opening
}

// TestHistoryAdapterKeepsExpiredFactsWithoutGrant proves history does not recreate transport authority or retain mutable wire objects.
// TestHistoryAdapterKeepsExpiredFactsWithoutGrant доказывает отсутствие нового transport authority и ссылок на mutable wire objects.
func TestHistoryAdapterKeepsExpiredFactsWithoutGrant(t *testing.T) {
	owner, client, binding, opening := historyAdapterFixture(t)
	result, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
	if err != nil || result.History == nil || result.Stream != nil || result.AcceptedResult != nil || !result.ReconcileOnly || !result.History.HardDeadline.Before(time.Now()) || client.beginEffectCalls != 1 {
		t.Fatal("historical evidence became fresh credit", err)
	}
	client.beginEffect.StreamHistory.HandshakeReceipt.Origin.SnapshotDigest = "changed"
	if result.History.Handshake.Origin.SnapshotDigest != strings.Repeat("a", 64) {
		t.Fatal("mutable wire response changed retained receipt")
	}
}

// TestHistoryAdapterRejectsForeignAndMixedVariants exercises schema, deadline, lineage and fresh-authority confusion.
// TestHistoryAdapterRejectsForeignAndMixedVariants проверяет schema, deadline, lineage и смешение с fresh authority.
func TestHistoryAdapterRejectsForeignAndMixedVariants(t *testing.T) {
	for _, scenario := range []string{"fresh", "grant", "resource", "unknown", "unknown-handshake", "unknown-origin", "state", "missing-deadline", "deadline-unknown", "extended", "generation", "foreign-binding", "foreign-child", "foreign-effect", "foreign-stream", "foreign-operation", "missing-handshake", "prepared-handshake", "completed-without-evidence"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening := historyAdapterFixture(t)
			history := client.beginEffect.StreamHistory
			switch scenario {
			case "fresh":
				client.beginEffect.ReconcileOnly = false
			case "grant":
				client.beginEffect.Stream = &cp.ManagedKubernetesOwnerStreamGrant{}
			case "resource":
				opening.Operation = "kubernetes.get"
			case "unknown":
				history.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "unknown-handshake":
				history.HandshakeReceipt.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "unknown-origin":
				history.HandshakeReceipt.Origin.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "state":
				history.State = cp.ManagedKubernetesOwnerStreamState(99)
			case "missing-deadline":
				history.HardDeadline = nil
			case "deadline-unknown":
				history.HardDeadline.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "extended":
				history.HardDeadline = timestamppb.New(opening.HardStreamDeadline.Add(time.Second))
			case "generation":
				history.Generation++
			case "foreign-binding":
				history.HandshakeReceipt.Origin.BindingDigest = strings.Repeat("f", 64)
			case "foreign-child":
				history.HandshakeReceipt.Origin.ChildInputDigest = strings.Repeat("f", 64)
			case "foreign-effect":
				history.HandshakeReceipt.Origin.EffectRef = "effect_foreign"
			case "foreign-stream":
				history.HandshakeReceipt.Origin.StreamRef = "stream_foreign"
			case "foreign-operation":
				history.HandshakeReceipt.Origin.Operation = "kubernetes.attach"
			case "missing-handshake":
				history.HandshakeReceipt = nil
			case "prepared-handshake":
				history.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_PREPARED
			case "completed-without-evidence":
				history.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED
			}
			result, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
			if err == nil || result.History != nil || result.Stream != nil || client.beginEffectCalls != 1 {
				t.Fatal("invalid history admitted or retried")
			}
		})
	}
}

// TestHistoryAdapterRetainsNativeTerminalAcrossOwnerOverride preserves a stored exit while rejecting malformed terminal evidence.
// TestHistoryAdapterRetainsNativeTerminalAcrossOwnerOverride сохраняет stored exit при owner override и отклоняет malformed terminal evidence.
func TestHistoryAdapterRetainsNativeTerminalAcrossOwnerOverride(t *testing.T) {
	for _, scenario := range []string{"valid", "accepted", "unknown", "handshake-digest", "missing-handshake", "foreign-origin", "outcome", "exit", "promoted-loss", "mixed-result"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening := historyAdapterFixture(t)
			history := client.beginEffect.StreamHistory
			origin, _ := kubernetesStreamOriginFromProto(history.HandshakeReceipt.Origin)
			handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: history.HandshakeReceipt.TransportRef, TransportGeneration: history.HandshakeReceipt.TransportGeneration, Protocol: history.HandshakeReceipt.Protocol, HTTPStatus: 101, NegotiatedProtocol: history.HandshakeReceipt.NegotiatedProtocol, PeerSPKISHA256: history.HandshakeReceipt.PeerSpkiSha256}
			digest, err := handshake.Digest()
			if err != nil {
				t.Fatal(err)
			}
			exit := int32(0)
			history.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_REVOKED
			history.TerminalReceipt = &cp.ManagedKubernetesTerminalReceipt{Origin: kubernetesStreamOriginToProto(origin), HandshakeDigest: digest, Protocol: handshake.Protocol, TransportJoined: true, LocalState: cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED, RemoteOutcome: cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED, ExitCode: &exit, ReasonCode: "REMOTE_EXIT"}
			switch scenario {
			case "accepted":
				accepted, buildErr := managedkubernetes.BuildStreamEffectResult(managedkubernetes.TerminalReceipt{Origin: origin, HandshakeDigest: digest, Protocol: handshake.Protocol, TransportJoined: true, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", ExitCode: &exit, ReasonCode: "REMOTE_EXIT"})
				if buildErr != nil {
					t.Fatal(buildErr)
				}
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(10 * time.Second))
				client.beginEffect.AcceptedResult = &cp.ManagedKubernetesAcceptedEffectResult{Outcome: cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED, ResultJson: accepted.ResultJSON, ResultDigest: accepted.ResultDigest}
			case "unknown":
				history.TerminalReceipt.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "handshake-digest":
				history.TerminalReceipt.HandshakeDigest = strings.Repeat("f", 64)
			case "missing-handshake":
				history.HandshakeReceipt = nil
			case "foreign-origin":
				history.TerminalReceipt.Origin.SnapshotDigest = strings.Repeat("f", 64)
			case "outcome":
				history.TerminalReceipt.RemoteOutcome = cp.ManagedKubernetesEffectOutcome(99)
			case "exit":
				exit = 1
			case "promoted-loss":
				history.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED
				history.TerminalReceipt.LocalState = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_LOST
				history.TerminalReceipt.ReasonCode = "OUTPUT_LOST"
			case "mixed-result":
				client.beginEffect.ExpiresAt = timestamppb.New(time.Now().Add(10 * time.Second))
				client.beginEffect.AcceptedResult = &cp.ManagedKubernetesAcceptedEffectResult{Outcome: cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED, ResultJson: []byte("{}"), ResultDigest: kubernetesDiscardedResultDigest}
			}
			result, err := owner.BeginKubernetesEffect(t.Context(), binding, opening)
			if scenario != "valid" && scenario != "accepted" {
				if err == nil || result.History != nil {
					t.Fatal("invalid terminal history accepted")
				}
				return
			}
			if err != nil || result.History == nil || result.History.State != "REVOKED" || result.History.Terminal == nil || result.History.Terminal.LocalState != "COMPLETED" || result.History.Terminal.RemoteOutcome != "SUCCEEDED" || result.Stream != nil {
				t.Fatal("owner override erased native truth or created credit", err)
			}
			if (result.AcceptedResult != nil) != (scenario == "accepted") {
				t.Fatal("fresh accepted result was lost or invented")
			}
			exit = 7
			if result.History.Terminal.ExitCode == nil || *result.History.Terminal.ExitCode != 0 {
				t.Fatal("wire mutation changed retained exit receipt")
			}
		})
	}
}
