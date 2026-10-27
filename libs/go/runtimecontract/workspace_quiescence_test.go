package runtimecontract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// workspaceQuiescenceFixture supplies synthetic current input and controller-observed UID bindings.
// workspaceQuiescenceFixture задаёт synthetic current input и observed controller UID bindings.
func workspaceQuiescenceFixture() (WorkspaceQuiescence, RunnerInput, WorkspaceStorageBinding) {
	manifest := workspaceArchiveFixture()
	writer := manifest.Writer
	writer.JoinSHA256 = ""
	record := WorkspaceQuiescence{Schema: WorkspaceQuiescenceSchema, Scope: manifest.Scope, WorkspaceRef: manifest.WorkspaceRef,
		Generation: manifest.Generation, Joined: true, Writer: writer}
	input := RunnerInput{Mode: RunnerModeTurn, OrganizationRef: manifest.Scope.OrganizationRef, ProjectRef: manifest.Scope.ProjectRef,
		SessionRef: manifest.Scope.SessionRef, AgentRef: manifest.Scope.AgentRef, RunRef: writer.RunRef, NodeRef: writer.NodeRef, TurnRef: writer.TurnRef,
		RuntimeRevisionRef: writer.RuntimeRevisionRef, RuntimeRevisionVersion: writer.RuntimeRevisionVersion, RuntimeRevisionDigest: writer.RuntimeRevisionDigest,
		ExecutionBindingDigest: writer.ExecutionBindingDigest, InputDigest: writer.InputDigest, LeaseRef: writer.LeaseRef,
		LeaseGeneration: writer.LeaseGeneration, LeaseFence: "synthetic-secret-fence", Attempt: writer.Attempt, WorkspacePolicy: RuntimeWorkspacePolicyV1()}
	binding := WorkspaceStorageBinding{Profile: WorkspaceStorageProfile, Scope: record.Scope, WorkspaceRef: record.WorkspaceRef, Generation: record.Generation}
	return record, input, binding
}

// TestWorkspaceQuiescenceRequiresExactOwnedExecution tests full tuple/UID matching, not claimed join truth.
// TestWorkspaceQuiescenceRequiresExactOwnedExecution проверяет exact tuple/UID matching, не истинность заявленного join.
func TestWorkspaceQuiescenceRequiresExactOwnedExecution(t *testing.T) {
	record, input, binding := workspaceQuiescenceFixture()
	raw, err := EncodeWorkspaceQuiescence(record)
	if err != nil || len(raw) > MaximumWorkspaceQuiescenceBytes || bytes.Contains(raw, []byte(input.LeaseFence)) {
		t.Fatal("bounded quiescence metadata rejected or disclosed a fence")
	}
	decoded, err := DecodeWorkspaceQuiescence(raw)
	if err != nil || !decoded.Matches(input, binding, record.Writer.PodUID, record.Writer.PVCUID) {
		t.Fatal("exact current generation and execution commitments rejected")
	}
	commitment, err := decoded.Commitment()
	if err != nil || commitment.JoinSHA256 != sha256Sum(raw) {
		t.Fatal("join commitment does not bind the complete canonical record")
	}
	for _, change := range []func(*WorkspaceQuiescence){
		func(value *WorkspaceQuiescence) { value.Scope.ProjectRef = "prj_foreign000" },
		func(value *WorkspaceQuiescence) { value.Generation++ },
		func(value *WorkspaceQuiescence) { value.WorkspaceRef = "wks_foreign000" },
		func(value *WorkspaceQuiescence) { value.Writer.PodUID = "foreign-pod-uid" },
		func(value *WorkspaceQuiescence) { value.Writer.PVCUID = "foreign-pvc-uid" },
		func(value *WorkspaceQuiescence) { value.Writer.Attempt++ },
		func(value *WorkspaceQuiescence) { value.Writer.LeaseGeneration++ },
		func(value *WorkspaceQuiescence) { value.Writer.InputDigest = strings.Repeat("0", 64) },
		func(value *WorkspaceQuiescence) { value.Writer.RuntimeRevisionVersion++ },
		func(value *WorkspaceQuiescence) { value.Joined = false },
	} {
		invalid := record
		change(&invalid)
		if invalid.Matches(input, binding, record.Writer.PodUID, record.Writer.PVCUID) {
			t.Fatal("foreign, stale or unjoined writer acquired a matching proof")
		}
	}
}

// TestWorkspaceQuiescenceNeverUpgradesMalformedTerminationBytes enforces the small closed record.
// TestWorkspaceQuiescenceNeverUpgradesMalformedTerminationBytes соблюдает малую closed record.
func TestWorkspaceQuiescenceNeverUpgradesMalformedTerminationBytes(t *testing.T) {
	record, _, _ := workspaceQuiescenceFixture()
	raw, err := EncodeWorkspaceQuiescence(record)
	if err != nil {
		t.Fatal("valid synthetic record rejected")
	}
	for _, malformed := range [][]byte{
		bytes.Replace(raw, []byte(`"joined":true`), []byte(`"joined":false,"joined":true`), 1),
		append(append([]byte{}, raw...), []byte(`{}`)...),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"provider_auth":"forbidden"}`)...),
		bytes.Repeat([]byte("x"), MaximumWorkspaceQuiescenceBytes+1),
	} {
		if _, err := DecodeWorkspaceQuiescence(malformed); err == nil {
			t.Fatal("malformed termination metadata became a positive proof")
		}
	}
	record.Joined = false
	unjoined, err := json.Marshal(record)
	if err != nil {
		t.Fatal("negative fixture cannot be serialized")
	}
	if _, err := DecodeWorkspaceQuiescence(unjoined); err == nil {
		t.Fatal("negative observation became a positive proof")
	}
	record.Joined = true
	record.Writer.JoinSHA256 = strings.Repeat("a", 64)
	if _, err := EncodeWorkspaceQuiescence(record); err == nil {
		t.Fatal("recursive self-hash entered the immutable source record")
	}
}
