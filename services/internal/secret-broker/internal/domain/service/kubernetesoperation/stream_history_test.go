package kubernetesoperation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
)

// TestLogsHistoryUsesAPIReceiptWithoutNodePin exercises actual Execute reconciliation with a retained HTTP log history.
// Foreign target revisions and privileged receipts must fail before owner closure, without opening any replacement stream.
// TestLogsHistoryUsesAPIReceiptWithoutNodePin проверяет actual Execute reconciliation с retained HTTP log history.
// Foreign target revisions и privileged receipts отвергаются до owner closure без открытия replacement stream.
func TestLogsHistoryUsesAPIReceiptWithoutNodePin(t *testing.T) {
	for _, variant := range []string{"valid", "foreign revision", "privileged receipt"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, factory, binding := streamHistoryFixture(t)
			snapshot := owner.resourceOwnerFixture.snapshot
			snapshot.Operation = "kubernetes.logs"
			snapshot.Input.Options = json.RawMessage(`{"timeoutSeconds":30}`)
			raw, err := managedkubernetes.EncodeOperationInput(snapshot.Operation, snapshot.Input)
			if err != nil {
				t.Fatal(err)
			}
			_, snapshot.NormalizedInput, err = managedkubernetes.DecodeOperationInput(snapshot.Operation, raw)
			if err != nil {
				t.Fatal(err)
			}
			owner.resourceOwnerFixture.snapshot = snapshot
			owner.streamResolverFixture.snapshot = snapshot
			child := &owner.plan.Children[0]
			child.Operation, child.Input, child.NormalizedInput = snapshot.Operation, snapshot.Input, snapshot.NormalizedInput
			child.InputDigest = resourceResultDigest(child.NormalizedInput)
			child.NodeTransport, child.Observation.PodWitness = nil, nil
			owner.admission.ChildInputDigest = child.InputDigest
			handshake := owner.admission.History.Handshake
			handshake.Origin.Operation, handshake.Origin.ChildInputDigest = child.Operation, child.InputDigest
			handshake.TransportRef, handshake.TransportGeneration = snapshot.Target.ID, int64(snapshot.Target.Revision)
			handshake.Protocol, handshake.HTTPStatus, handshake.NegotiatedProtocol = managedkubernetes.HTTPLogsV1, 200, ""
			if variant == "foreign revision" {
				handshake.TransportGeneration++
			}
			if variant == "privileged receipt" {
				handshake.Protocol = string(managedkubernetes.WebSocketV5)
			}
			result, err := service.Execute(t.Context(), binding)
			if variant == "valid" {
				if err != nil || !result.Effect.ReconcileOnly || result.Stream != nil || len(owner.closed) != 1 || len(owner.completions) != 1 {
					t.Fatal("HTTP history failed reconciliation", err)
				}
			} else if err == nil || len(owner.closed) != 0 || len(owner.completions) != 0 {
				t.Fatal("foreign log receipt reached closure")
			}
			if factory.preparations != 0 || factory.opens != 0 {
				t.Fatal("history opened replacement log stream")
			}
		})
	}
}

// TestWatchHistoryUsesAPIReceiptWithoutNodePin exercises actual Execute reconciliation with a retained HTTP collection watch history.
// Foreign target revisions and privileged receipts must fail before owner closure, without opening any replacement stream.
// TestWatchHistoryUsesAPIReceiptWithoutNodePin проверяет actual Execute reconciliation с retained HTTP collection watch history.
// Foreign target revisions и privileged receipts отвергаются до owner closure без открытия replacement stream.
func TestWatchHistoryUsesAPIReceiptWithoutNodePin(t *testing.T) {
	for _, variant := range []string{"valid", "foreign revision", "privileged receipt"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, factory, binding := streamHistoryFixture(t)
			snapshot := owner.resourceOwnerFixture.snapshot
			snapshot.Operation = "kubernetes.watch"
			snapshot.Input.Name, snapshot.Input.UID, snapshot.Input.ResourceVersion = "", "", ""
			snapshot.Input.Options = json.RawMessage(`{"timeoutSeconds":30}`)
			raw, err := managedkubernetes.EncodeOperationInput(snapshot.Operation, snapshot.Input)
			if err != nil {
				t.Fatal(err)
			}
			_, snapshot.NormalizedInput, err = managedkubernetes.DecodeOperationInput(snapshot.Operation, raw)
			if err != nil {
				t.Fatal(err)
			}
			owner.resourceOwnerFixture.snapshot = snapshot
			owner.streamResolverFixture.snapshot = snapshot
			child := &owner.plan.Children[0]
			child.Operation, child.Input, child.NormalizedInput = snapshot.Operation, snapshot.Input, snapshot.NormalizedInput
			child.InputDigest = resourceResultDigest(child.NormalizedInput)
			child.NodeTransport, child.Observation.PodWitness = nil, nil
			child.Resource.Name, child.Resource.UID, child.Resource.ResourceVersion = "", "", ""
			child.Observation.Resource = child.Resource
			child.Observation.ObservationState = managedkubernetes.ObservationCollection
			owner.admission.ChildInputDigest = child.InputDigest
			handshake := owner.admission.History.Handshake
			handshake.Origin.Operation, handshake.Origin.ChildInputDigest = child.Operation, child.InputDigest
			handshake.TransportRef, handshake.TransportGeneration = snapshot.Target.ID, int64(snapshot.Target.Revision)
			handshake.Protocol, handshake.HTTPStatus, handshake.NegotiatedProtocol = managedkubernetes.HTTPWatchV1, 200, ""
			if variant == "foreign revision" {
				handshake.TransportGeneration++
			}
			if variant == "privileged receipt" {
				handshake.Protocol = string(managedkubernetes.WebSocketV5)
			}
			result, err := service.Execute(t.Context(), binding)
			if variant == "valid" {
				if err != nil || !result.Effect.ReconcileOnly || result.Stream != nil || len(owner.closed) != 1 || len(owner.completions) != 1 {
					t.Fatal("HTTP history failed reconciliation", err)
				}
			} else if err == nil || len(owner.closed) != 0 || len(owner.completions) != 0 {
				t.Fatal("foreign watch receipt reached closure")
			}
			if factory.preparations != 0 || factory.opens != 0 {
				t.Fatal("history opened replacement watch stream")
			}
		})
	}
}

// streamHistoryFixture retains an attempted owner child with a missing local registry and no target preparation.
// streamHistoryFixture сохраняет attempted owner child без local registry и target preparation.
func streamHistoryFixture(t *testing.T) (*StreamOperations, *streamPlanFixture, *streamPreparationFixture, managedkubernetes.Binding) {
	t.Helper()
	service, owner, factory, binding := streamOperationsFixture(t)
	child := &owner.plan.Children[0]
	child.State = "ATTEMPTING"
	child.NodeTransport.Generation = factory.transport.handshake.TransportGeneration
	child.NodeTransport.ServerPublicKeySHA256 = factory.transport.handshake.PeerSPKISHA256
	owner.admission.ReconcileOnly = true
	owner.admission.Stream = nil
	handshake := factory.transport.handshake
	owner.admission.History = &targetrepo.StreamHistory{Ref: owner.grant.Ref, Generation: binding.Generation, State: "ACTIVE", HardDeadline: owner.grant.ExpiresAt, Handshake: &handshake}
	return service, owner, factory, binding
}

// TestStreamHistoryRecoveryDoesNotReopenOrInventJoin proves lost routing is UNKNOWN even if another replica might retain a writer.
// TestStreamHistoryRecoveryDoesNotReopenOrInventJoin доказывает UNKNOWN при потере маршрута, даже если writer остался в другой replica.
func TestStreamHistoryRecoveryDoesNotReopenOrInventJoin(t *testing.T) {
	for _, scenario := range []string{"active", "prepared", "closed-unreported", "stored-terminal"} {
		t.Run(scenario, func(t *testing.T) {
			service, owner, factory, binding := streamHistoryFixture(t)
			history := owner.admission.History
			if scenario == "prepared" || scenario == "closed-unreported" {
				history.State, history.Handshake = "PREPARED", nil
			}
			if scenario == "closed-unreported" {
				history.State, owner.finalState = "REVOKED", "REVOKED"
			}
			if scenario == "stored-terminal" {
				terminal := failedOpenedStream(*history.Handshake)
				history.Terminal, history.State, owner.finalState = &terminal, "REVOKED", "REVOKED"
			}
			result, err := service.Execute(t.Context(), binding)
			if err != nil || !result.Effect.ReconcileOnly || result.Effect.Result != nil || result.Stream != nil || !result.Effect.AuthorizedUntil.IsZero() {
				t.Fatal("recovery manufactured result or fresh dispatch", err)
			}
			if factory.observations != 0 || factory.preparations != 0 || factory.opens != 0 || owner.opens != 0 {
				t.Fatal("history recovery touched private target or reopened transport")
			}
			select {
			case closed := <-owner.closed:
				if closed.ExpectedState != history.State || closed.RemoteOutcome != "UNKNOWN" || closed.TransportJoined != (scenario == "stored-terminal") || closed.ObservedExitCode != nil {
					t.Fatal("history recovery invented native facts")
				}
				if history.Handshake == nil && (closed.TerminalReceipt.HandshakeDigest != "" || closed.TerminalReceipt.ReasonCode != "OWNER_STATE_LOST") {
					t.Fatal("unreported handshake became a proven failure")
				}
			default:
				t.Fatal("owner stream was not closed")
			}
			select {
			case completed := <-owner.completions:
				if completed.Outcome != "UNKNOWN" || completed.StreamRef != history.Ref {
					t.Fatal("unknown history became business success")
				}
			default:
				t.Fatal("terminal effect was not reconciled")
			}
		})
	}
}

// TestStreamHistoryRejectsForeignEvidenceAndLostOwnerACK prevents foreign history or uncertain closure from completing an effect.
// TestStreamHistoryRejectsForeignEvidenceAndLostOwnerACK не даёт чужой history либо uncertain closure завершить effect.
func TestStreamHistoryRejectsForeignEvidenceAndLostOwnerACK(t *testing.T) {
	for _, scenario := range []string{"generation", "snapshot", "node-pin", "unknown-state", "missing-handshake", "promoted-terminal", "lost-close"} {
		t.Run(scenario, func(t *testing.T) {
			service, owner, factory, binding := streamHistoryFixture(t)
			history := owner.admission.History
			switch scenario {
			case "generation":
				history.Generation++
			case "snapshot":
				history.Handshake.Origin.SnapshotDigest = strings.Repeat("9", 64)
			case "node-pin":
				history.Handshake.PeerSPKISHA256 = strings.Repeat("9", 64)
			case "unknown-state":
				history.State = "NEW_STATE"
			case "missing-handshake":
				history.Handshake = nil
			case "promoted-terminal":
				terminal := failedOpenedStream(*history.Handshake)
				history.Terminal, history.State = &terminal, "COMPLETED"
			case "lost-close":
				owner.closeErr = errors.New("synthetic lost owner acknowledgement")
			}
			result, err := service.Execute(t.Context(), binding)
			if err == nil || result.Stream != nil || result.Effect.Result != nil || factory.preparations != 0 || factory.opens != 0 || len(owner.completions) != 0 {
				t.Fatal("invalid or uncertain history completed/reopened an effect")
			}
			if scenario != "lost-close" && len(owner.closed) != 0 {
				t.Fatal("foreign history reached owner close")
			}
		})
	}
}
