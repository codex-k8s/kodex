package model

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// workspaceSnapshotTaskFixture constructs a closed synthetic task with a positive record but no owner grant.
// It tests private wire interpretation; these values are not an observed Pod/PVC or stored receipt.
// workspaceSnapshotTaskFixture создаёт closed synthetic task с positive record, без owner grant.
// Проверяется private wire; значения не являются observed Pod/PVC или stored receipt.
func workspaceSnapshotTaskFixture(t *testing.T) Task {
	t.Helper()
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	writer := runtimecontract.WorkspaceWriterCommitment{RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000",
		RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64),
		ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_fixture000",
		LeaseGeneration: 1, Attempt: 1, PodUID: "pod-fixture", PVCUID: "pvc-fixture"}
	joined, err := runtimecontract.EncodeWorkspaceQuiescence(runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema,
		Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2, Joined: true, Writer: writer})
	if err != nil {
		t.Fatal(err)
	}
	pvc, err := runtimecontract.SessionPVCName(scope.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{TaskRef: "sat_fixture000", Kind: "SNAPSHOT", OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef,
		SessionRef: scope.SessionRef, RuntimeRevisionRef: writer.RuntimeRevisionRef, RuntimeRevisionVersion: writer.RuntimeRevisionVersion,
		RuntimeRevisionDigest: writer.RuntimeRevisionDigest, ContentGeneration: 7, PVCName: pvc, InputDigest: strings.Repeat("d", 64), Attempt: 1,
		Workspace: &WorkspaceArchiveTask{Target: runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
			Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2}, CheckpointRef: "wcp_fixture000", Quiescence: string(joined),
			PVCUID: writer.PVCUID, Policy: runtimecontract.RuntimeWorkspacePolicyV1()}}
	identity, err := task.Workspace.SnapshotIdentity()
	if err != nil {
		t.Fatal(err)
	}
	task.TargetObjectKey = identity.ObjectPrefix() + "manifest.json"
	return task
}

// TestWorkspaceTaskWireIsClosedAndCannotDowngrade checks actual Task marshal/unmarshal and legacy byte preservation.
// New tasks reject duplicate, alias-case, null, unknown and trailing input before any materialization.
// TestWorkspaceTaskWireIsClosedAndCannotDowngrade проверяет actual Task codec и legacy byte preservation.
// New tasks отклоняют duplicate, alias-case, null, unknown и trailing input до materialization.
func TestWorkspaceTaskWireIsClosedAndCannotDowngrade(t *testing.T) {
	task := workspaceSnapshotTaskFixture(t)
	raw, err := json.Marshal(task)
	if err != nil || len(raw) > MaximumTaskBytes || task.Validate() != nil {
		t.Fatal("valid work-only task rejected")
	}
	var decoded Task
	if json.Unmarshal(raw, &decoded) != nil || decoded.Validate() != nil || decoded.Workspace == nil || decoded.CodexSessionID != "" {
		t.Fatal("work-only task lost binding or invented Codex identity")
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"Workspace":`), []byte(`"Workspace":null,"Workspace":`), 1),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"Workspace":null}`)...),
		bytes.Replace(raw, []byte(`"Workspace":`), []byte(`"workspace":`), 1),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"authority":true}`)...),
		append(append([]byte{}, raw...), []byte(`{}`)...),
	} {
		if json.Unmarshal(invalid, &decoded) == nil {
			t.Fatal("ambiguous new task was interpreted as legacy or valid work")
		}
	}
	legacy, err := FromProto(validSnapshotProto())
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw, err := json.Marshal(legacy)
	if err != nil || bytes.Contains(legacyRaw, []byte(`"Workspace"`)) {
		t.Fatal("nil storage changed the legacy task wire")
	}
	var oldShape struct {
		TaskRef, Kind, OrganizationRef, ProjectRef, SessionRef string
		ProviderAccountRef, RuntimeRevisionRef                 string
		RuntimeRevisionVersion, ContentGeneration              int64
		RuntimeRevisionDigest, CodexSessionID                  string
		PVCName, InputDigest                                   string
		SourceRelativePath, SourceSHA256                       string
		SourceSizeBytes                                        int64
		TargetObjectKey, TargetObjectVersion                   string
		Attempt                                                int32
		Archive                                                *ArchiveBinding
	}
	if json.Unmarshal(legacyRaw, &oldShape) != nil {
		t.Fatal("legacy task wire could not be read")
	}
	before, err := json.Marshal(oldShape)
	if err != nil || !bytes.Equal(before, legacyRaw) {
		t.Fatal("optional workspace changed legacy task bytes")
	}
}

// TestWorkspaceTaskRequiresExactRecordAndReadOnlyRecoveryCredit prevents new attempts from silently retrying PUT.
// Metadata equality is still not authorization; owner task/lease checks remain mandatory.
// TestWorkspaceTaskRequiresExactRecordAndReadOnlyRecoveryCredit запрещает silent PUT retry новыми attempts.
// Metadata equality не является authorization; owner task/lease checks обязательны отдельно.
func TestWorkspaceTaskRequiresExactRecordAndReadOnlyRecoveryCredit(t *testing.T) {
	for _, name := range []string{"pvc", "generation", "record", "revision", "key", "codex", "retry_credit"} {
		t.Run(name, func(t *testing.T) {
			task := workspaceSnapshotTaskFixture(t)
			switch name {
			case "pvc":
				task.Workspace.PVCUID = "different-pvc"
			case "generation":
				task.Workspace.Target.Generation++
			case "record":
				task.Workspace.Quiescence = "{}"
			case "revision":
				task.RuntimeRevisionVersion++
			case "key":
				task.TargetObjectKey = "session-archive/v2/foreign/manifest.json"
			case "codex":
				task.CodexSessionID = "synthetic-unrelated-identity"
			case "retry_credit":
				task.Attempt++
			}
			if task.Validate() == nil {
				t.Fatal("changed writer/task pins received valid work authority")
			}
		})
	}
	task := workspaceSnapshotTaskFixture(t)
	task.Attempt, task.Workspace.ReconcileOnly = 2, true
	if task.Validate() != nil {
		t.Fatal("explicit read-only reconciliation attempt rejected")
	}
}
