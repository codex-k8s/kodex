package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// workspaceObservationFixture builds synthetic immutable owner metadata for pure receipt projection tests.
// It does not simulate SQL authority, a real Pod, process quiescence or a checkpoint acknowledgement.
// workspaceObservationFixture создаёт synthetic immutable owner metadata для pure receipt projection tests.
// SQL authority, real Pod, process quiescence и checkpoint acknowledgement не имитируются.
func workspaceObservationFixture(t *testing.T) (workspaceObservationLease, command.Result, string) {
	t.Helper()
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone,
		RoleRuntimeContractRevision: runtimecontract.WorkspaceStorageRuntimeContractRevision, OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef, SessionRef: scope.SessionRef, AgentRef: scope.AgentRef,
		RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000", RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_fixture000", LeaseGeneration: 1, Attempt: 1,
		WorkspaceStorage: &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile, Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 1}}
	ack := runtimecontract.WorkspaceWriterAcknowledgement{Schema: runtimecontract.WorkspaceWriterAcknowledgementSchema, Scope: scope, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, Generation: 1,
		Writer: runtimecontract.WorkspaceWriterCommitment{RunRef: input.RunRef, NodeRef: input.NodeRef, TurnRef: input.TurnRef, RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion,
			RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, Attempt: input.Attempt, PodUID: "pod-fixture", PVCUID: "pvc-fixture"}}
	raw, err := runtimecontract.EncodeWorkspaceWriterAcknowledgement(ack)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := runtimecontract.EncodeWorkspaceQuiescence(runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema, Scope: scope, WorkspaceRef: ack.WorkspaceRef, Generation: 1, Joined: true, Writer: ack.Writer})
	if err != nil {
		t.Fatal(err)
	}
	locked := workspaceObservationLease{storageRef: ack.WorkspaceRef, storageGeneration: 1, storageState: "BLOCKED", blockReason: "POD_UNREACHABLE", writerLeaseID: "exact-lease", leaseID: "exact-lease", ackRaw: string(raw), input: input}
	result := command.Result{Runtime: map[string]any{"workspaceWriterAck": string(raw), "workspaceCommittedPhase": runtimecontract.WorkspaceObserveBlocked, "workspaceCommittedQuiescence": ""}}
	return locked, result, string(joined)
}

// TestWorkspaceReceiptLateJoinUsesCurrentTupleWithoutRewritingOutcome checks the lost-ACK/late-join projection seam.
// The old BLOCKED receipt stays immutable while current readback exposes the newly confirmed same writer.
// TestWorkspaceReceiptLateJoinUsesCurrentTupleWithoutRewritingOutcome проверяет lost-ACK/late-join projection seam.
// Старый BLOCKED receipt неизменен; current readback показывает подтверждённого same writer.
func TestWorkspaceReceiptLateJoinUsesCurrentTupleWithoutRewritingOutcome(t *testing.T) {
	locked, receipt, joined := workspaceObservationFixture(t)
	before, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	payload := command.WorkspaceObservationInput{WorkspaceGeneration: 1}
	first, err := workspaceObservationCurrentReadback(locked, payload, receipt)
	if err != nil || first.Runtime["workspaceStorageState"] != "BLOCKED" || first.Runtime["workspaceCurrentTuple"] != true {
		t.Fatal("initial current readback rejected")
	}
	locked.storageState, locked.blockReason, locked.joinRaw = "CHECKPOINT_PENDING", "", joined
	current, err := workspaceObservationCurrentReadback(locked, payload, receipt)
	if err != nil || current.Runtime["workspaceStorageState"] != "CHECKPOINT_PENDING" || current.Runtime["workspaceQuiescence"] != joined || current.Runtime["workspaceCommittedPhase"] != runtimecontract.WorkspaceObserveBlocked || current.Runtime["workspaceCommittedQuiescence"] != "" {
		t.Fatal("late join was hidden or historical BLOCKED outcome was rewritten")
	}
	after, err := json.Marshal(receipt)
	if err != nil || string(before) != string(after) {
		t.Fatal("readback mutated receipt metadata")
	}
	locked.storageGeneration++
	historical, err := workspaceObservationCurrentReadback(locked, payload, current)
	if err != nil || historical.Runtime["workspaceCurrentTuple"] != false || historical.Runtime["workspaceStorageState"] != nil || historical.Runtime["workspaceQuiescence"] != nil || historical.Runtime["workspaceWriterAck"] != receipt.Runtime["workspaceWriterAck"] {
		t.Fatal("another generation borrowed old current authority")
	}
}

// TestWorkspaceMissingPodWithoutPositiveJoinCannotRelease exercises the actual owner handler before any SQL write.
// Missing/corrupt join remains forbidden even when a caller possesses exact frozen acknowledgement fields.
// TestWorkspaceMissingPodWithoutPositiveJoinCannotRelease проверяет actual owner handler до любого SQL write.
// Missing/corrupt join остаётся forbidden даже при exact frozen acknowledgement fields у caller.
func TestWorkspaceMissingPodWithoutPositiveJoinCannotRelease(t *testing.T) {
	locked, _, _ := workspaceObservationFixture(t)
	repository := &Repository{}
	for _, joined := range []string{"", "{}", "null"} {
		locked.joinRaw = joined
		if _, err := repository.observeWorkspaceReleased(context.Background(), nil, scope{}, locked); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("missing positive join did not fail before SQL: %v", err)
		}
	}
}

// TestWorkspaceReceiptStoresOnlyCommittedFields prevents cached current phase from masquerading as authority.
// TestWorkspaceReceiptStoresOnlyCommittedFields запрещает cached current phase выглядеть authority.
func TestWorkspaceReceiptStoresOnlyCommittedFields(t *testing.T) {
	_, result, joined := workspaceObservationFixture(t)
	result.Runtime["workspaceCommittedPhase"], result.Runtime["workspaceCommittedQuiescence"] = runtimecontract.WorkspaceObserveReleased, joined
	result.Runtime["workspaceStorageState"], result.Runtime["workspaceWriterReleased"], result.Runtime["workspaceCurrentTuple"] = "CHECKPOINT_PENDING", true, true
	stored := workspaceObservationReceiptMetadata(command.ObserveWorkspace, result)
	if len(stored.Runtime) != 3 || stored.Runtime["workspaceCommittedPhase"] != runtimecontract.WorkspaceObserveReleased || stored.Runtime["workspaceCommittedQuiescence"] != joined || stored.Runtime["workspaceStorageState"] != nil || stored.Runtime["workspaceCurrentTuple"] != nil {
		t.Fatal("receipt retained mutable current metadata or lost immutable release outcome")
	}
}
