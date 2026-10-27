package codex

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// workspaceJoinSchema identifies a private broker fact produced after Wait/readers/process-group join.
// workspaceJoinSchema обозначает private broker fact после Wait/readers/process-group join.
const workspaceJoinSchema = "kodex.provider-workspace-join.v1"

// workspaceJoinProof is not reconstructed from exported Result fields; sealing requires trusted peer readback.
// The provider creates it only after captureReady, independently of archive availability or business outcome.
// workspaceJoinProof не восстанавливается из exported Result fields; sealing требует trusted peer readback.
// Provider создаёт proof лишь после captureReady, независимо от archive или business outcome.
type workspaceJoinProof struct {
	Schema                 string `json:"schema"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	InputDigest            string `json:"input_digest"`
	LeaseRef               string `json:"lease_ref"`
	LeaseGeneration        int64  `json:"lease_generation"`
	Attempt                int32  `json:"attempt"`
	Joined                 bool   `json:"joined"`
	sealed                 bool
}

// newWorkspaceJoinProof binds a positively stopped provider to the immutable V2 execution.
// This private constructor does not stop a process; its sole production caller first proves captureReady.
// newWorkspaceJoinProof связывает positively stopped provider с immutable V2 execution.
// Constructor не останавливает процесс; единственный production caller сначала доказывает captureReady.
func newWorkspaceJoinProof(input model.Input) *workspaceJoinProof {
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil ||
		!validCaptureDigest(input.ExecutionBindingDigest) || !validCaptureDigest(input.RuntimeRevisionDigest) || !validCaptureDigest(input.InputDigest) ||
		input.LeaseRef == "" || input.LeaseGeneration < 1 || input.Attempt < 1 {
		return nil
	}
	return &workspaceJoinProof{Schema: workspaceJoinSchema, ExecutionBindingDigest: input.ExecutionBindingDigest,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef,
		LeaseGeneration: input.LeaseGeneration, Attempt: input.Attempt, Joined: true, sealed: true}
}

// bound checks exact input equality without treating decoded metadata as authenticated evidence.
// bound проверяет exact input equality, не считая decoded metadata authenticated evidence.
func (proof *workspaceJoinProof) bound(input model.Input) bool {
	expected := newWorkspaceJoinProof(input)
	if proof == nil || expected == nil {
		return false
	}
	actual := *proof
	actual.sealed = true
	return actual == *expected
}

// WorkspaceQuiescence returns a closed record only from sealed process evidence and an exact owner ACK.
// Arbitrary JSON/exported Result fields cannot manufacture this past-fact storage observation.
// WorkspaceQuiescence возвращает closed record лишь из sealed process evidence и exact owner ACK.
// Arbitrary JSON/exported Result fields не создают past-fact storage observation.
func (result Result) WorkspaceQuiescence(input model.Input, ack runtimecontract.WorkspaceWriterAcknowledgement) (runtimecontract.WorkspaceQuiescence, bool) {
	if result.workspaceJoin == nil || !result.workspaceJoin.sealed || !result.workspaceJoin.bound(input) || !ack.Matches(input) {
		return runtimecontract.WorkspaceQuiescence{}, false
	}
	value := runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema, Scope: ack.Scope,
		WorkspaceRef: ack.WorkspaceRef, Generation: ack.Generation, Joined: true, Writer: ack.Writer}
	return value, value.Validate() == nil
}
