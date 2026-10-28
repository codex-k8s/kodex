package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testWorkspaceRestoreOwner runs a new same-session model request through the exact accepted source checkpoint.
// Worker observations are explicit disposable fixtures; this proves owner transitions, not physical PVC/S3 effects.
// testWorkspaceRestoreOwner проводит новый model request той же сессии через exact accepted source checkpoint.
// Worker observations — disposable fixtures; проверяются owner transitions, не physical PVC/S3 effects.
func testWorkspaceRestoreOwner(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, pool *pgxpool.Pool,
	owner, worker, observer value.Principal, previous *runtimecontract.WorkspaceStorageBinding, oldBind command.WorkspaceObservationInput,
	execute func(command.Kind, value.Principal, string, any) command.Result,
) {
	t.Helper()
	run := execute(command.LaunchRun, owner, "restore-launch", command.LaunchRunInput{
		ProjectRef: previous.Scope.ProjectRef, SessionRef: previous.Scope.SessionRef, Title: "Продолжить работу с файлами",
		Task: "Проверить сохранённый сервис", Target: entity.RunTarget{Type: "AGENT", Ref: previous.Scope.AgentRef}}).Run
	if run == nil || run.SessionRef != previous.Scope.SessionRef {
		t.Fatal("restore request did not retain exact session")
	}
	for attempt := 0; attempt < 2; attempt++ {
		pending := execute(command.ClaimExecution, worker, fmt.Sprintf("restore-pending-%d", attempt),
			command.LeaseInput{WorkloadInstance: "workspace-restore-controller", Limit: 1})
		if len(pending.RuntimeItems) != 0 {
			t.Fatal("model was admitted before exact restore completion")
		}
	}
	var state string
	var generation int64
	var count int
	if err := pool.QueryRow(ctx, `SELECT w.state,w.generation,(SELECT count(*) FROM control_plane.session_archive_tasks task
		WHERE task.workspace_id=w.id AND task.kind='RESTORE') FROM control_plane.session_workspace_storage w WHERE w.ref=$1`,
		previous.WorkspaceRef).Scan(&state, &generation, &count); err != nil || state != "RESTORE_READY" || generation != previous.Generation+1 || count != 1 {
		t.Fatalf("restore intent mismatch: state=%s generation=%d count=%d error=%v", state, generation, count, err)
	}
	claimer := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.claim")
	items, err := service.ClaimSessionArchiveTasks(ctx, claimer, "workspace-restore-fixture", 1)
	if err != nil || len(items) != 1 {
		t.Fatal("restore owner did not issue exactly one storage claim", err)
	}
	work, ok := items[0]["workspaceWork"].(runtimecontract.WorkspaceArchiveWork)
	if !ok || work.Validate() != nil || work.Kind != "RESTORE" || work.Attempt != 1 || work.Workspace.ReconcileOnly ||
		work.Workspace.Target.Generation != generation || work.Workspace.Target.VolumeReady == nil ||
		work.Workspace.Checkpoint == nil || work.Workspace.Checkpoint.Generation != previous.Generation ||
		work.Workspace.Checkpoint.WorkspaceRef != previous.WorkspaceRef || work.Workspace.Checkpoint.Scope != previous.Scope {
		t.Fatal("restore input did not pin exact source, target and prepared volume")
	}
	lease := command.SessionArchiveTaskInput{TaskRef: work.TaskRef, LeaseRef: stringMap(items[0], "leaseRef"),
		Fence: stringMap(items[0], "fence"), Generation: runtimeRevisionMapInt64(items[0], "generation")}
	renewer := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.renew")
	if _, err := service.RenewSessionArchiveTask(ctx, renewer, lease); err != nil {
		t.Fatal("renew exact restore grant", err)
	}
	source := work.Workspace.Checkpoint
	payload := lease
	payload.FormatVersion, payload.ObjectKey, payload.ObjectVersion = 2, source.Manifest.Key, source.Manifest.Version
	payload.ObjectETag, payload.ObjectDigest, payload.ObjectSizeBytes = source.Manifest.ETag, source.Manifest.Digest, source.Manifest.SizeBytes
	payload.RestoredSourceSHA256, payload.SourceSizeBytes = source.TreeSHA256, source.Bytes
	completer := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.restore.complete")
	for index, change := range []func(*command.SessionArchiveTaskInput){
		func(v *command.SessionArchiveTaskInput) { v.FormatVersion = 1 },
		func(v *command.SessionArchiveTaskInput) { v.ObjectVersion += "-other" },
		func(v *command.SessionArchiveTaskInput) { v.ObjectDigest = "sha256:invalid" },
		func(v *command.SessionArchiveTaskInput) { v.RestoredSourceSHA256 = "" },
		func(v *command.SessionArchiveTaskInput) { v.SourceSizeBytes++ },
		func(v *command.SessionArchiveTaskInput) { v.SourceEntryCount = 1 },
	} {
		changed := payload
		change(&changed)
		if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionRestore, Principal: completer,
			Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("restore-invalid-%d", index)}, Payload: changed}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("restore accepted mismatched immutable metadata", index, err)
		}
	}
	completed := execute(command.CompleteSessionRestore, completer, "restore-complete", payload)
	replayed := execute(command.CompleteSessionRestore, completer, "restore-complete", payload)
	if stringMap(completed.Runtime, "state") != "SUCCEEDED" || stringMap(completed.Runtime, "archiveRef") == "" ||
		stringMap(completed.Runtime, "archiveRef") != stringMap(replayed.Runtime, "archiveRef") {
		t.Fatal("restore completion replay changed its accepted source")
	}
	if _, err := service.RenewSessionArchiveTask(ctx, renewer, lease); err == nil {
		t.Fatal("completed restore lease retained authority")
	}
	claimed := execute(command.ClaimExecution, worker, "restore-model-claim", command.LeaseInput{WorkloadInstance: "workspace-restore-controller", Limit: 1})
	if len(claimed.RuntimeItems) != 1 {
		t.Fatal("restored tree did not unblock the current model request")
	}
	binding, err := runtimeWorkspaceSnapshotBinding(claimed.RuntimeItems[0])
	if err != nil || binding == nil || binding.Generation != generation || binding.Source == nil || *binding.Source != *source ||
		binding.WorkspaceRef != previous.WorkspaceRef || binding.VolumeReady == nil {
		t.Fatal("fresh runtime revision lost accepted restore provenance", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ObserveWorkspace, Principal: observer,
		Mutation: value.Mutation{IdempotencyKey: "restore-old-bind"}, Payload: oldBind}); err == nil {
		t.Fatal("previous generation writer rebound the restored tree")
	}
	newBind := oldBind
	newBind.LeaseRef, newBind.Fence = stringMap(claimed.RuntimeItems[0], "leaseRef"), stringMap(claimed.RuntimeItems[0], "fence")
	newBind.Generation, newBind.RuntimeRevisionDigest = runtimeRevisionMapInt64(claimed.RuntimeItems[0], "generation"), stringMap(claimed.RuntimeItems[0], "revisionDigest")
	newBind.WorkspaceGeneration = generation
	newBind.PodUID = "00000000-0000-4000-8000-000000000029"
	bound := execute(command.ObserveWorkspace, observer, "restore-fresh-bind", newBind)
	if stringMap(bound.Runtime, "workspaceStorageState") != "WRITING" {
		t.Fatal("current generation could not bind its restored workspace")
	}
	current, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal("read restored run before fixture cancellation", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "restore-finish-fixture", ExpectedVersion: &current.Version},
		Payload:  command.RunCommandInput{RunRef: current.Ref}}); err != nil {
		t.Fatal("cancel restored fixture run", err)
	}
}
