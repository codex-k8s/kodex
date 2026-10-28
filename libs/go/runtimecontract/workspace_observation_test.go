package runtimecontract

import (
	"bytes"
	"strings"
	"testing"
)

// TestWorkspaceObservationNeverTurnsQuiescenceIntoFreshAuthority checks the closed two-phase wire.
// No codec result alone proves owner ACK, terminal containers, PVC ownership or writer permission.
// TestWorkspaceObservationNeverTurnsQuiescenceIntoFreshAuthority проверяет closed two-phase wire.
// Codec не доказывает owner ACK, terminal containers, PVC ownership или writer permission.
func TestWorkspaceObservationNeverTurnsQuiescenceIntoFreshAuthority(t *testing.T) {
	joined, input, binding := workspaceQuiescenceFixture()
	input.WorkspaceStorage = &binding
	observation := RunnerWorkspaceObservation{RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		Phase: WorkspaceObserveBind, WorkspaceRef: binding.WorkspaceRef, Generation: binding.Generation}
	if observation.ValidateForInput(input) != nil {
		t.Fatal("exact initial BIND metadata rejected")
	}
	observation.Phase = WorkspaceObserveQuiescent
	if observation.ValidateForInput(input) == nil {
		t.Fatal("missing join became a positive observation")
	}
	observation.Quiescence = &joined
	if observation.ValidateForInput(input) != nil {
		t.Fatal("exact positive record rejected")
	}
	observation.Phase = WorkspaceObserveBind
	if observation.ValidateForInput(input) == nil {
		t.Fatal("past quiescence record was reinterpreted as a fresh BIND")
	}
	observation.Phase = "RENEW"
	if observation.ValidateForInput(input) == nil {
		t.Fatal("workspace observation widened into execution renewal")
	}
	observation.Phase = WorkspaceObserveQuiescent
	joined.Writer.LeaseGeneration++
	if observation.ValidateForInput(input) == nil {
		t.Fatal("old record matched a different lease generation")
	}
}

// TestWorkspaceOwnerAcknowledgementIsClosedAndSecretFree checks the actual canonical ACK codec.
// Bad digests, recursive join hashes and unknown fields never reach filesystem materialization.
// TestWorkspaceOwnerAcknowledgementIsClosedAndSecretFree проверяет canonical ACK codec.
// Bad digest, recursive join hash и unknown field не допускаются к filesystem materialization.
func TestWorkspaceOwnerAcknowledgementIsClosedAndSecretFree(t *testing.T) {
	joined, input, binding := workspaceQuiescenceFixture()
	input.WorkspaceStorage = &binding
	ack := WorkspaceWriterAcknowledgement{Schema: WorkspaceWriterAcknowledgementSchema, Scope: binding.Scope,
		WorkspaceRef: binding.WorkspaceRef, Generation: binding.Generation, Writer: joined.Writer}
	raw, err := EncodeWorkspaceWriterAcknowledgement(ack)
	if err != nil || bytes.Contains(raw, []byte(input.LeaseFence)) {
		t.Fatal("bounded ACK rejected or disclosed a private fence")
	}
	decoded, err := DecodeWorkspaceWriterAcknowledgement(raw)
	if err != nil || !decoded.Matches(input) {
		t.Fatal("current immutable owner ACK did not match its input")
	}
	if decoded.Writer.Matches(input) {
		t.Fatal("initial owner ACK was promoted into a positive joined-writer proof")
	}
	for _, change := range []func(*WorkspaceWriterAcknowledgement){
		func(value *WorkspaceWriterAcknowledgement) { value.Writer.LeaseGeneration++ },
		func(value *WorkspaceWriterAcknowledgement) { value.Writer.Attempt++ },
		func(value *WorkspaceWriterAcknowledgement) { value.Writer.InputDigest = strings.Repeat("0", 64) },
		func(value *WorkspaceWriterAcknowledgement) { value.Writer.RuntimeRevisionRef = "rrv_foreign000" },
		func(value *WorkspaceWriterAcknowledgement) { value.Generation++ },
		func(value *WorkspaceWriterAcknowledgement) { value.Scope.ProjectRef = "prj_foreign000" },
	} {
		invalid := ack
		change(&invalid)
		if invalid.Matches(input) {
			t.Fatal("initial owner ACK accepted another execution tuple or generation")
		}
	}
	ack.Writer.JoinSHA256 = strings.Repeat("0", 64)
	if _, err := EncodeWorkspaceWriterAcknowledgement(ack); err == nil {
		t.Fatal("writer ACK fabricated a positive join")
	}
	for _, invalid := range [][]byte{
		append(append([]byte(nil), raw...), []byte(`{}`)...),
		append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"secret":"forbidden"}`)...),
		bytes.Replace(raw, []byte(`"generation":1`), []byte(`"generation":0,"generation":1`), 1),
	} {
		if _, err := DecodeWorkspaceWriterAcknowledgement(invalid); err == nil {
			t.Fatal("malformed owner ACK became a valid materialization input")
		}
	}
}

// TestWorkspaceV2RequiresPinnedRoleABIAndClosedBlockCauses rejects opt-in on an older image contract.
// Closed BLOCKED causes contain neither arbitrary errors nor a claim that an absent Pod stopped.
// TestWorkspaceV2RequiresPinnedRoleABIAndClosedBlockCauses запрещает opt-in на старом image contract.
// Closed BLOCKED causes не содержат arbitrary errors и не доказывают остановку отсутствующего Pod.
func TestWorkspaceV2RequiresPinnedRoleABIAndClosedBlockCauses(t *testing.T) {
	_, input, binding := workspaceQuiescenceFixture()
	for _, revision := range []uint64{0, 1, 3, 5} {
		input.RoleRuntimeContractRevision = revision
		if binding.ValidateForInput(input) == nil {
			t.Fatal("unsupported role ABI accepted V2 work")
		}
	}
	input.RoleRuntimeContractRevision = WorkspaceStorageRuntimeContractRevision
	if binding.ValidateForInput(input) != nil {
		t.Fatal("V2 role ABI rejected")
	}
	for _, reason := range []string{"POD_MISSING", "POD_UNREACHABLE", "PVC_MISSING", "PVC_REPLACED", "OWNERSHIP_CHANGED", "WRITER_JOIN_UNCONFIRMED"} {
		if !ValidWorkspaceBlockReason(reason) {
			t.Fatal("closed block cause rejected")
		}
	}
	for _, reason := range []string{"", "UNKNOWN", "Pod not found", "QUIESCENT", "../other", "POD_MISSING\n"} {
		if ValidWorkspaceBlockReason(reason) {
			t.Fatal("unclosed cause became storage authority")
		}
	}
}
