package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// workspaceJoinFixture supplies synthetic pins for broker provenance tests, not a process-stop witness.
// Production sealing still requires captureReady; these tests isolate peer/schema/tuple enforcement.
// workspaceJoinFixture задаёт synthetic pins для broker provenance tests, не process-stop witness.
// Production sealing требует captureReady; tests отдельно проверяют peer/schema/tuple enforcement.
func workspaceJoinFixture() (model.Input, runtimecontract.WorkspaceWriterAcknowledgement) {
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	input := model.Input{RoleRuntimeContractRevision: runtimecontract.WorkspaceStorageRuntimeContractRevision, Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone,
		OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef, SessionRef: scope.SessionRef, AgentRef: scope.AgentRef,
		RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000", RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_fixture000", LeaseGeneration: 1, Attempt: 1,
		WorkspaceStorage: &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile, Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 1}}
	ack := runtimecontract.WorkspaceWriterAcknowledgement{Schema: runtimecontract.WorkspaceWriterAcknowledgementSchema, Scope: scope, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, Generation: 1,
		Writer: runtimecontract.WorkspaceWriterCommitment{RunRef: input.RunRef, NodeRef: input.NodeRef, TurnRef: input.TurnRef, RuntimeRevisionRef: input.RuntimeRevisionRef,
			RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest, InputDigest: input.InputDigest,
			LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, Attempt: input.Attempt, PodUID: "pod-fixture", PVCUID: "pvc-fixture"}}
	return input, ack
}

// TestWorkspaceJoinCannotBeForgedByPublicResult proves JSON/exported fields lose the private sealed witness.
// TestWorkspaceJoinCannotBeForgedByPublicResult доказывает отсутствие private sealed witness после public JSON.
func TestWorkspaceJoinCannotBeForgedByPublicResult(t *testing.T) {
	input, ack := workspaceJoinFixture()
	result := Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic", workspaceJoin: newWorkspaceJoinProof(input)}
	if _, ok := result.WorkspaceQuiescence(input, ack); !ok {
		t.Fatal("private synthetic witness rejected")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("workspace_join")) {
		t.Fatal("private witness leaked through Result JSON")
	}
	var public Result
	if json.Unmarshal(raw, &public) != nil {
		t.Fatal("Result could not be decoded")
	}
	if _, ok := public.WorkspaceQuiescence(input, ack); ok {
		t.Fatal("public result manufactured quiescence")
	}
	changed := input
	changed.LeaseGeneration++
	if _, ok := result.WorkspaceQuiescence(changed, ack); ok {
		t.Fatal("old private witness reopened a new generation")
	}
}

// TestWorkspaceJoinBrokerRequiresTrustedPeerAndExactTuple checks actual terminal serialization and strict reader.
// Missing input, wrong UID, changed lease and malformed/unknown fields never publish a usable witness.
// TestWorkspaceJoinBrokerRequiresTrustedPeerAndExactTuple проверяет actual terminal serialization и strict reader.
// Missing input, wrong UID, changed lease и malformed/unknown fields не создают usable witness.
func TestWorkspaceJoinBrokerRequiresTrustedPeerAndExactTuple(t *testing.T) {
	input, ack := workspaceJoinFixture()
	result := Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic", workspaceJoin: newWorkspaceJoinProof(input)}
	var stream bytes.Buffer
	writer := &brokerFrameWriter{writer: &stream, input: &input}
	if err := writer.finish(brokerResponse{Result: result, OK: true}); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), stream.Bytes()...)
	got, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &input, providerWriterUID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.WorkspaceQuiescence(input, ack); !ok {
		t.Fatal("trusted terminal did not preserve exact witness")
	}
	for _, uid := range []uint32{0, 10001} {
		if _, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &input, uid); err == nil {
			t.Fatal("wrong peer UID accepted")
		}
	}
	if _, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), nil, providerWriterUID); err == nil {
		t.Fatal("unbound witness accepted")
	}
	changed := input
	changed.LeaseGeneration++
	if _, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &changed, providerWriterUID); err == nil {
		t.Fatal("stale execution tuple accepted")
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"joined":true`), []byte(`"joined":false`), 1),
		bytes.Replace(raw, []byte(`"workspace_join":{`), []byte(`"workspace_join":{"extra":true,`), 1),
		bytes.Replace(raw, []byte(`"workspace_join":{`), []byte(`"workspace_join":{"joined":true,`), 1),
	} {
		if _, err := readBoundProviderBrokerResponse(bytes.NewReader(bad), &input, providerWriterUID); err == nil {
			t.Fatal("malformed join accepted")
		}
	}
}
