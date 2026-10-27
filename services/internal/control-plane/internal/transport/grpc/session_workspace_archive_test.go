package grpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

// workspaceArchiveOwnerFixture assigns bounded metadata for transport tests without creating an owner grant.
// workspaceArchiveOwnerFixture назначает bounded metadata для transport tests без создания owner grant.
func workspaceArchiveOwnerFixture(t *testing.T, kind string) runtimecontract.WorkspaceArchiveWork {
	t.Helper()
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	writer := runtimecontract.WorkspaceWriterCommitment{RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000",
		RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64),
		ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64), LeaseRef: "lease_fixture000",
		LeaseGeneration: 1, Attempt: 1, PodUID: "pod-fixture", PVCUID: "pvc-fixture"}
	joined, err := runtimecontract.EncodeWorkspaceQuiescence(runtimecontract.WorkspaceQuiescence{Schema: runtimecontract.WorkspaceQuiescenceSchema,
		Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2, Joined: true, Writer: writer})
	if err != nil {
		t.Fatal("encode archive writer fixture", err)
	}
	pvc, err := runtimecontract.SessionPVCName(scope.SessionRef)
	if err != nil {
		t.Fatal("resolve archive PVC fixture", err)
	}
	work := runtimecontract.WorkspaceArchiveWork{TaskRef: "sat_fixture000", Kind: kind, OrganizationRef: scope.OrganizationRef,
		ProjectRef: scope.ProjectRef, SessionRef: scope.SessionRef, RuntimeRevisionRef: writer.RuntimeRevisionRef,
		RuntimeRevisionVersion: writer.RuntimeRevisionVersion, RuntimeRevisionDigest: writer.RuntimeRevisionDigest,
		ContentGeneration: 7, PVCName: pvc, InputDigest: strings.Repeat("d", 64), Attempt: 1,
		Workspace: &runtimecontract.WorkspaceArchiveTask{Target: runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
			Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2}, CheckpointRef: "wcp_fixture000", Quiescence: string(joined),
			PVCUID: writer.PVCUID, Policy: runtimecontract.RuntimeWorkspacePolicyV1()}}
	identity, err := work.Workspace.SnapshotIdentity()
	if err != nil {
		t.Fatal("resolve snapshot identity fixture", err)
	}
	if kind == "SNAPSHOT" {
		work.TargetObjectKey = identity.ObjectPrefix() + "manifest.json"
		return work
	}
	generation := int64(2)
	if kind == "RESTORE" {
		generation = 1
	}
	checkpoint := &runtimecontract.WorkspaceCheckpointBinding{Scope: scope, WorkspaceRef: "wks_fixture000", Generation: generation,
		CheckpointRef: "wcp_fixture000", TreeSHA256: strings.Repeat("e", 64), Bytes: 3, Entries: 1,
		Manifest: runtimecontract.WorkspaceObjectCommitment{Version: "version-fixture", ETag: "etag-fixture", Digest: "sha256:" + strings.Repeat("f", 64), SizeBytes: 2048}}
	checkpoint.Manifest.Key = checkpoint.ObjectPrefix() + "manifest.json"
	work.Workspace.Quiescence, work.Workspace.Checkpoint = "", checkpoint
	if kind == "DELETE_OBJECT" {
		work.TargetObjectKey, work.TargetObjectVersion = checkpoint.Manifest.Key, checkpoint.Manifest.Version
	}
	return work
}

// TestWorkspaceArchiveOwnerWire exercises the actual owner caster and exports its exact protobuf bytes only on request.
// The explicit local wire harness passes these bytes to the independent worker module; default unit tests still validate all cases.
// TestWorkspaceArchiveOwnerWire проверяет actual owner caster и экспортирует exact protobuf bytes только по запросу.
// Явная local wire оснастка передаёт bytes независимому worker module; обычный unit проверяет все cases.
func TestWorkspaceArchiveOwnerWire(t *testing.T) {
	fixtures := make(map[string][]byte)
	for _, kind := range []string{"SNAPSHOT", "RESTORE", "DELETE_PVC", "DELETE_OBJECT"} {
		work := workspaceArchiveOwnerFixture(t, kind)
		if err := work.Validate(); err != nil {
			t.Fatal("owner fixture invalid", kind, err)
		}
		result := castSessionArchiveTask(map[string]any{"workspaceWork": work})
		if result == nil || result.Workspace == nil || result.CodexSessionId != "" || result.SourceRelativePath != "" ||
			result.Workspace.Target.Scope.AgentRef != work.Workspace.Target.Scope.AgentRef ||
			result.Workspace.Policy.Digest != work.Workspace.Policy.Digest {
			t.Fatal("owner lost or downgraded workspace task", kind)
		}
		raw, err := proto.Marshal(result)
		if err != nil {
			t.Fatal("marshal owner task", err)
		}
		fixtures[kind] = raw
	}
	for _, kind := range []string{"SNAPSHOT", "RESTORE"} {
		work := workspaceArchiveOwnerFixture(t, kind)
		work.Attempt, work.Workspace.ReconcileOnly = 2, true
		result := castSessionArchiveTask(map[string]any{"workspaceWork": work})
		if result == nil || !result.Workspace.ReconcileOnly {
			t.Fatal("owner lost read-only reconciliation credit", kind)
		}
		raw, err := proto.Marshal(result)
		if err != nil {
			t.Fatal("marshal reconciliation task", err)
		}
		fixtures[kind+"_RECONCILE"] = raw
	}
	for _, value := range []any{nil, (*runtimecontract.WorkspaceArchiveWork)(nil), map[string]any{}, runtimecontract.WorkspaceArchiveWork{}} {
		if castSessionArchiveTask(map[string]any{"workspaceWork": value}) != nil {
			t.Fatal("invalid assigned workspace was downgraded to legacy")
		}
	}
	for _, mutate := range []func(*runtimecontract.WorkspaceArchiveWork){
		func(w *runtimecontract.WorkspaceArchiveWork) { w.SessionRef = "ses_foreign000" },
		func(w *runtimecontract.WorkspaceArchiveWork) { w.Workspace.Policy.MaximumWritableBytes++ },
		func(w *runtimecontract.WorkspaceArchiveWork) { w.RuntimeRevisionDigest = strings.Repeat("0", 64) },
		func(w *runtimecontract.WorkspaceArchiveWork) { w.Attempt = 2 },
		func(w *runtimecontract.WorkspaceArchiveWork) { w.TargetObjectKey += "/other" },
	} {
		work := workspaceArchiveOwnerFixture(t, "SNAPSHOT")
		mutate(&work)
		if castSessionArchiveTask(map[string]any{"workspaceWork": work}) != nil {
			t.Fatal("mismatched workspace task admitted")
		}
	}
	legacy := castSessionArchiveTask(map[string]any{"taskRef": "sat_legacy000", "kind": "SNAPSHOT", "codexSessionID": "legacy-codex"})
	if legacy == nil || legacy.Workspace != nil || legacy.CodexSessionId != "legacy-codex" {
		t.Fatal("legacy projection changed")
	}
	path := os.Getenv("KODEX_WORKSPACE_OWNER_PROTO_FIXTURE")
	if path == "" {
		path = filepath.Join(t.TempDir(), "owner-tasks.json")
	}
	raw, err := json.Marshal(fixtures)
	if err != nil || len(raw) > runtimecontract.MaximumWorkspaceManifestBytes {
		t.Fatal("encode bounded owner wire fixture")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create private owner wire fixture", err)
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		t.Fatal("write private owner wire fixture", err)
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil || closeErr != nil {
		t.Fatal("finish private owner wire fixture")
	}
}
