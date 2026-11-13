package app

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

// TestUnboundWorkspaceReceiptRejectsMixedAuthority checks a lost-response read rejoin without
// granting authority from another tuple, mutable phase, positive ACK or noncanonical bytes.
// TestUnboundWorkspaceReceiptRejectsMixedAuthority проверяет lost-response read rejoin без
// выдачи authority из другого tuple, mutable phase, positive ACK или noncanonical bytes.
func TestUnboundWorkspaceReceiptRejectsMixedAuthority(t *testing.T) {
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_synthetic000", ProjectRef: "prj_synthetic000", SessionRef: "ses_synthetic000", AgentRef: "agt_synthetic000"}
	writer := runtimecontract.WorkspaceWriterCommitment{RunRef: "run_synthetic000", NodeRef: "nod_synthetic000", TurnRef: "trn_synthetic000", RuntimeRevisionRef: "rrv_synthetic000", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_synthetic000", LeaseGeneration: 1, Attempt: 1, PodUID: "20000000-0000-4000-8000-000000000001", PVCUID: "30000000-0000-4000-8000-000000000001"}
	input := runtimecontract.RunnerInput{RoleRuntimeContractRevision: runtimecontract.WorkspaceStorageRuntimeContractRevision, Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef, SessionRef: scope.SessionRef, AgentRef: scope.AgentRef, RunRef: writer.RunRef, NodeRef: writer.NodeRef, TurnRef: writer.TurnRef, RuntimeRevisionRef: writer.RuntimeRevisionRef, RuntimeRevisionVersion: 1, RuntimeRevisionDigest: writer.RuntimeRevisionDigest, ExecutionBindingDigest: writer.ExecutionBindingDigest, InputDigest: writer.InputDigest, LeaseRef: writer.LeaseRef, LeaseGeneration: 1, Attempt: 1, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(), WorkspaceStorage: &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile, Scope: scope, WorkspaceRef: "wks_synthetic000", Generation: 1}}
	fact := runtimecontract.WorkspaceUnboundStop{Schema: runtimecontract.WorkspaceUnboundStopSchema, Scope: scope, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, Generation: 1, NodeName: "synthetic-node", NodeUID: "40000000-0000-4000-8000-000000000001", Evidence: "ALL_UNSTARTED", Execution: writer}
	raw, err := runtimecontract.EncodeWorkspaceUnboundStop(fact)
	if err != nil {
		t.Fatal(err)
	}
	base := &controlplanev1.ObserveWorkspaceResponse{CommittedPhase: runtimecontract.WorkspaceObserveUnboundStop, UnboundStop: raw}
	for _, variant := range []string{"exact", "nil", "foreign-project", "foreign-lease", "wrong-phase", "ack", "join", "committed-join", "current", "released", "state", "reason", "noncanonical"} {
		t.Run(variant, func(t *testing.T) {
			response := proto.Clone(base).(*controlplanev1.ObserveWorkspaceResponse)
			changed := fact
			switch variant {
			case "nil":
				response = nil
			case "foreign-project":
				changed.Scope.ProjectRef = "prj_other000"
				response.UnboundStop, _ = runtimecontract.EncodeWorkspaceUnboundStop(changed)
			case "foreign-lease":
				changed.Execution.LeaseRef = "lease_other000"
				response.UnboundStop, _ = runtimecontract.EncodeWorkspaceUnboundStop(changed)
			case "wrong-phase":
				response.CommittedPhase = runtimecontract.WorkspaceObserveReleased
			case "ack":
				response.WriterAcknowledgement = []byte("synthetic")
			case "join":
				response.Quiescence = []byte("synthetic")
			case "committed-join":
				response.CommittedQuiescence = []byte("synthetic")
			case "current":
				response.CurrentTuple = true
			case "released":
				response.WriterReleased = true
			case "state":
				response.StorageState = "EMPTY"
			case "reason":
				response.BlockReason = "POD_MISSING"
			case "noncanonical":
				response.UnboundStop = append(response.UnboundStop, ' ')
			}
			got, err := unboundWorkspaceReceipt(response, input)
			if (err == nil) != (variant == "exact") || err == nil && got != fact {
				t.Fatal("invalid unbound cleanup permission")
			}
		})
	}
}
