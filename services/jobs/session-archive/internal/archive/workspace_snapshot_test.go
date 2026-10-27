package archive

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

// workspaceSnapshotFixture supplies valid private scope syntax and a real canonical encoder without production grants.
// Owner and two-UID mount proof remain separate from this publication fault contour.
// workspaceSnapshotFixture задаёт valid private scope и настоящий canonical encoder без production grants.
// Owner и two-UID mount proof проверяются отдельно от этого контура сбоев публикации.
func workspaceSnapshotFixture(t *testing.T) (model.Task, workspaceSnapshotEncoder) {
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
	task := model.Task{TaskRef: "sat_fixture000", Kind: "SNAPSHOT", OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef,
		SessionRef: scope.SessionRef, RuntimeRevisionRef: writer.RuntimeRevisionRef, RuntimeRevisionVersion: writer.RuntimeRevisionVersion,
		RuntimeRevisionDigest: writer.RuntimeRevisionDigest, ContentGeneration: 7, PVCName: pvc, InputDigest: strings.Repeat("d", 64), Attempt: 1,
		Workspace: &model.WorkspaceArchiveTask{Target: runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
			Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 2}, CheckpointRef: "wcp_fixture000", Quiescence: string(joined),
			PVCUID: writer.PVCUID, Policy: runtimecontract.RuntimeWorkspacePolicyV1()}}
	identity, err := task.Workspace.SnapshotIdentity()
	if err != nil {
		t.Fatal(err)
	}
	task.TargetObjectKey = identity.ObjectPrefix() + "manifest.json"
	encode := func(ctx context.Context, destination io.Writer, task model.Task) (string, int64, int64, int64, error) {
		encoder, err := newWorkspaceStreamEncoder(ctx, destination, task.Workspace.Policy)
		if err != nil {
			return "", 0, 0, 0, err
		}
		if err := encoder.appendOwnedEntry("source.go", false, 0o600, 12, strings.NewReader("package main"), workspaceStreamProviderOwner); err != nil {
			return "", 0, 0, 0, err
		}
		return encoder.finish()
	}
	return task, encode
}

// workspacePublicationStore records actual publication order while retaining the storage fault adapter.
// workspacePublicationStore фиксирует фактический порядок публикации с прежним storage fault adapter.
type workspacePublicationStore struct {
	*workspaceObjectFixture
	keys []string
}

// Put records the attempted key even when its acknowledgement is lost.
// Put учитывает попытку записи ключа даже при потерянном подтверждении.
func (store *workspacePublicationStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	store.keys = append(store.keys, input.Key)
	return store.workspaceObjectFixture.Put(ctx, input)
}

// TestWorkspaceSnapshotPublishesManifestLastAndRejoinsWithoutWrite checks actual canonical bytes and exact receipts.
// Lost PUT acknowledgements are reconciled by reads; retry neither creates a version nor removes prior data.
// TestWorkspaceSnapshotPublishesManifestLastAndRejoinsWithoutWrite проверяет canonical bytes и exact receipts.
// Lost PUT ACK разрешается чтением; retry не создаёт версию и не удаляет прежние данные.
func TestWorkspaceSnapshotPublishesManifestLastAndRejoinsWithoutWrite(t *testing.T) {
	task, encode := workspaceSnapshotFixture(t)
	store := &workspacePublicationStore{workspaceObjectFixture: &workspaceObjectFixture{Store: objectstoragetest.New(), losePutACK: true}}
	first, err := snapshotWorkspace(t.Context(), store, task, encode)
	if err != nil || !first.Success || first.FormatVersion != 2 || first.SourceSizeBytes != 12 || len(store.keys) != 2 ||
		!strings.HasSuffix(store.keys[0], "/parts/000001.tar") || store.keys[1] != task.TargetObjectKey || store.deletes != 0 {
		t.Fatal("complete work did not publish parts before the exact root receipt")
	}
	object, err := store.Get(t.Context(), first.ObjectKey, first.ObjectVersion)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	manifest, decodeErr := runtimecontract.DecodeWorkspaceArchiveManifest(raw)
	if err != nil || closeErr != nil || decodeErr != nil || manifest.TreeSHA256 != first.SourceSHA256 || manifest.Bytes != 12 || manifest.Entries != 1 {
		t.Fatal("root receipt does not commit the complete canonical tree")
	}
	task.Attempt = 2
	task.Workspace.ReconcileOnly = true
	again, err := snapshotWorkspace(t.Context(), store, task, encode)
	if err != nil || first != again || len(store.keys) != 2 || store.deletes != 0 {
		t.Fatal("read-only recovery changed the checkpoint or repeated a write")
	}
}

// TestWorkspaceSnapshotUnknownPartNeverPublishesRootOrRetriesWrite preserves unknown custody across attempts.
// A later NotFound cannot turn reconciliation credit into a new PUT permission.
// TestWorkspaceSnapshotUnknownPartNeverPublishesRootOrRetriesWrite сохраняет unknown custody между attempts.
// Последующий NotFound не превращает reconciliation credit в разрешение нового PUT.
func TestWorkspaceSnapshotUnknownPartNeverPublishesRootOrRetriesWrite(t *testing.T) {
	task, encode := workspaceSnapshotFixture(t)
	store := &workspacePublicationStore{workspaceObjectFixture: &workspaceObjectFixture{Store: objectstoragetest.New(), skipCommit: true}}
	for attempt := int32(1); attempt <= 2; attempt++ {
		task.Attempt, task.Workspace.ReconcileOnly = attempt, attempt > 1
		if result, err := snapshotWorkspace(t.Context(), store, task, encode); err == nil || result.Success || len(store.keys) != 1 || store.deletes != 0 {
			t.Fatal("unknown part produced root custody, another PUT or speculative DELETE")
		}
	}
}
