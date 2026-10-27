package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testWorkspaceCheckpointOwner exercises persisted tasks and atomic metadata acceptance after real writer commands.
// Receipts are trusted worker fixtures here; actual source objects and PVC bytes are tested by the independent worker/live contour.
// testWorkspaceCheckpointOwner проверяет persisted tasks и atomic metadata acceptance после настоящих writer commands.
// Receipts здесь trusted worker fixtures; actual source objects и PVC bytes проверяет отдельный worker/live contour.
func testWorkspaceCheckpointOwner(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, pool *pgxpool.Pool,
	owner, claimPrincipal value.Principal, binding *runtimecontract.WorkspaceStorageBinding, runRef string,
) {
	t.Helper()
	claim := func() (runtimecontract.WorkspaceArchiveWork, command.SessionArchiveTaskInput) {
		t.Helper()
		items, err := service.ClaimSessionArchiveTasks(ctx, claimPrincipal, "workspace-checkpoint-fixture", 1)
		if err != nil || len(items) != 1 {
			t.Fatal("claim persisted workspace task", err)
		}
		work, ok := items[0]["workspaceWork"].(runtimecontract.WorkspaceArchiveWork)
		if !ok || work.Validate() != nil || work.Workspace.Target.WorkspaceRef != binding.WorkspaceRef {
			t.Fatal("owner task lost immutable typed source")
		}
		return work, command.SessionArchiveTaskInput{TaskRef: work.TaskRef, LeaseRef: stringMap(items[0], "leaseRef"),
			Fence: stringMap(items[0], "fence"), Generation: runtimeRevisionMapInt64(items[0], "generation")}
	}
	work, lease := claim()
	renewer := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.renew")
	snapshotter := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.snapshot.complete")
	failer := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.fail")
	t.Run("checkpoint_task_and_renew", func(t *testing.T) {
		if work.Attempt != 1 || work.Workspace.ReconcileOnly || work.ContentGeneration < 1 {
			t.Fatal("initial snapshot grant was not a first attempt")
		}
		if _, err := service.RenewSessionArchiveTask(ctx, renewer, lease); err != nil {
			t.Fatal("renew exact workspace archive grant", err)
		}
	})
	failed := lease
	failed.SafeErrorCode = "SESSION_ARCHIVE_OBJECT_WRITE_FAILED"
	if !t.Run("checkpoint_retry_is_reconciliation_only", func(t *testing.T) {
		result, err := service.Execute(ctx, command.Command{Kind: command.FailSessionArchiveTask, Principal: failer,
			Mutation: value.Mutation{IdempotencyKey: "workspace-checkpoint-fail"}, Payload: failed})
		if err != nil || stringMap(result.Runtime, "state") != "READY" || result.Runtime["retryScheduled"] != true {
			t.Fatal("retain failed checkpoint intent", err)
		}
		// Only the disposable queue clock is advanced; no grant, lineage or business state is manufactured.
		// Меняется только disposable queue clock; grant, lineage и business state не создаются вручную.
		if _, err := pool.Exec(ctx, "UPDATE control_plane.session_archive_tasks SET available_at=clock_timestamp()-interval '1 second' WHERE ref=$1 AND state='READY'", work.TaskRef); err != nil {
			t.Fatal("advance disposable retry availability", err)
		}
	}) {
		return
	}
	previousWork, previousLease := work, lease
	work, lease = claim()
	if work.TaskRef != previousWork.TaskRef || work.Workspace.CheckpointRef != previousWork.Workspace.CheckpointRef ||
		work.TargetObjectKey != previousWork.TargetObjectKey || work.ContentGeneration != previousWork.ContentGeneration ||
		work.Attempt != 2 || !work.Workspace.ReconcileOnly || lease.Generation != previousLease.Generation+1 || work.InputDigest == previousWork.InputDigest {
		t.Fatal("retry changed logical checkpoint or did not issue fresh fenced read-only input")
	}
	t.Run("checkpoint_stale_lease_rejected", func(t *testing.T) {
		if _, err := service.RenewSessionArchiveTask(ctx, renewer, previousLease); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("old archive grant renewed after retry", err)
		}
		replayed, err := service.Execute(ctx, command.Command{Kind: command.FailSessionArchiveTask, Principal: failer,
			Mutation: value.Mutation{IdempotencyKey: "workspace-checkpoint-fail"}, Payload: failed})
		if err != nil || stringMap(replayed.Runtime, "state") != "READY" {
			t.Fatal("historical failed-attempt receipt was not replayed", err)
		}
		var generation int64
		var state string
		if err := pool.QueryRow(ctx, "SELECT state,generation FROM control_plane.session_archive_tasks WHERE ref=$1", work.TaskRef).Scan(&state, &generation); err != nil ||
			state != "CLAIMED" || generation != lease.Generation {
			t.Fatal("old failure replay revoked the new attempt", err)
		}
	})
	payload := lease
	payload.FormatVersion, payload.ObjectKey, payload.ObjectVersion, payload.ObjectETag = 2, work.TargetObjectKey, "version-fixture", "etag-fixture"
	payload.ObjectDigest, payload.ObjectSizeBytes = "sha256:"+strings.Repeat("a", 64), 2048
	payload.SnapshotSourceSHA256, payload.SourceSizeBytes, payload.SourceEntryCount = strings.Repeat("b", 64), 12, 1
	t.Run("checkpoint_invalid_metadata_rejected", func(t *testing.T) {
		for index, mutate := range []func(*command.SessionArchiveTaskInput){
			func(p *command.SessionArchiveTaskInput) { p.FormatVersion = 1 },
			func(p *command.SessionArchiveTaskInput) { p.ObjectKey += "/other" },
			func(p *command.SessionArchiveTaskInput) { p.ObjectVersion = "" },
			func(p *command.SessionArchiveTaskInput) { p.SnapshotSourceSHA256 = "" },
			func(p *command.SessionArchiveTaskInput) { p.SourceEntryCount = 0 },
			func(p *command.SessionArchiveTaskInput) { p.PVCName = "unexpected" },
		} {
			changed := payload
			mutate(&changed)
			if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotter,
				Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("workspace-checkpoint-invalid-%d", index)}, Payload: changed}); !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("invalid workspace checkpoint accepted", index, err)
			}
		}
	})
	t.Run("checkpoint_complete_replay_and_no_pvc_delete", func(t *testing.T) {
		input := command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotter,
			Mutation: value.Mutation{IdempotencyKey: "workspace-checkpoint-complete"}, Payload: payload}
		result, err := service.Execute(ctx, input)
		if err != nil || stringMap(result.Runtime, "archiveRef") == "" || stringMap(result.Runtime, "state") != "SUCCEEDED" {
			t.Fatal("complete private workspace checkpoint", err)
		}
		replayed, err := service.Execute(ctx, input)
		if err != nil || stringMap(replayed.Runtime, "archiveRef") != stringMap(result.Runtime, "archiveRef") {
			t.Fatal("checkpoint acceptance replay changed receipt", err)
		}
		var state string
		var format, deletes, receipts int
		var noInventedCodex bool
		if err := pool.QueryRow(ctx, `SELECT w.state,a.format_version,a.codex_session_id IS NULL AND a.source_relative_path IS NULL,
			(SELECT count(*) FROM control_plane.session_archive_tasks t WHERE t.session_id=w.session_id AND t.kind='DELETE_PVC'),
			(SELECT count(*) FROM control_plane.session_archives r WHERE r.workspace_id=w.id)
			FROM control_plane.session_workspace_storage w JOIN control_plane.session_archives a ON a.id=w.current_archive_id WHERE w.ref=$1`,
			binding.WorkspaceRef).Scan(&state, &format, &noInventedCodex, &deletes, &receipts); err != nil ||
			state != "CHECKPOINT_READY" || format != 2 || !noInventedCodex || deletes != 0 || receipts != 1 {
			t.Fatal("checkpoint metadata was partial or granted whole-PVC deletion", err)
		}
		items, err := service.ClaimSessionArchiveTasks(ctx, claimPrincipal, "workspace-checkpoint-fixture", 1)
		if err != nil || len(items) != 0 {
			t.Fatal("accepted checkpoint created another logical archive action", err)
		}
		run, err := service.GetRun(ctx, owner, runRef)
		if err != nil || run.State != "CANCELLED" {
			t.Fatal("checkpoint acceptance changed cancelled business result", err)
		}
	})
}
