package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testSessionArchiveLifecycle(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	runtimeWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct session archive service fixture: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-project"}, Payload: command.ProjectInput{
			Name: "Archive lifecycle", Purpose: "Verify session snapshot and restore", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create session archive project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "session-archive-agent", "Archive specialist")
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Create archive fixture", Task: "Create an immutable session archive fixture.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
		}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch session archive run: run=%#v err=%v", launched.Run, err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: runtimeWorker,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-runtime-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: "session-archive-runtime", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim session archive runtime: claims=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	const codexSessionID = "00000000-0000-4000-8000-000000000003"
	const sourcePath = ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-00000000-0000-4000-8000-000000000003.jsonl"
	const sourceSize = int64(128)
	completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: runtimeWorker,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-runtime-complete"}, Payload: command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Success: true, ResultSummary: "Archive fixture prepared", Usage: turnUsageFixture(), CodexSessionID: codexSessionID,
			ArchiveRelativePath: sourcePath, ArchiveSHA256: strings.Repeat("a", 64), ArchiveSizeBytes: sourceSize,
		}})
	if err != nil || completed.Run == nil || completed.Run.State != "SUCCEEDED" {
		t.Fatalf("complete session archive runtime: run=%#v err=%v", completed.Run, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_storage SET idle_since = clock_timestamp() - interval '1 hour' WHERE session_id = (SELECT id FROM control_plane.sessions WHERE ref = $1)", launched.Run.SessionRef); err != nil {
		t.Fatalf("make session archive fixture idle: %v", err)
	}

	claimPrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.claim")
	snapshotPrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.snapshot.complete")
	pvcPrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.pvc-delete.complete")
	restorePrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.restore.complete")
	objectPrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.object-delete.complete")
	failPrincipal := sessionArchivePrincipal(t, ctx, repository, "platform.session-archive.tasks.fail")
	if _, err := pool.Exec(ctx, querySessionArchiveMaterializeTasks, pgx.StrictNamedArgs{
		"idle_seconds": int64(sessionArchiveIdleAfter.Seconds()), "retention_seconds": int64(sessionArchiveRetention.Seconds()),
		"maximum_attempts": sessionArchiveMaxAttempts,
	}); err != nil {
		t.Fatalf("materialize session archive fixture: %v", err)
	}

	snapshot := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
	snapshotPayload := claimedSessionArchivePayload(snapshot)
	snapshotPayload.FormatVersion = 1
	snapshotPayload.ObjectKey = stringMap(snapshot, "objectKey")
	snapshotPayload.ObjectVersion = "seaweed-version-1"
	snapshotPayload.ObjectETag = "session-archive-etag"
	snapshotPayload.ObjectDigest = "sha256:" + strings.Repeat("b", 64)
	snapshotPayload.ObjectSizeBytes = sourceSize + 1024
	snapshotPayload.SourceSizeBytes = sourceSize
	stale := snapshotPayload
	stale.Fence = "fnc_stale_fence"
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotPrincipal,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-stale-snapshot"}, Payload: stale}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("stale session snapshot fence was accepted: %v", err)
	}
	snapshotResult, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotPrincipal,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-snapshot-complete"}, Payload: snapshotPayload})
	if err != nil || stringMap(snapshotResult.Runtime, "archiveRef") == "" {
		t.Fatalf("complete session snapshot: result=%#v err=%v", snapshotResult.Runtime, err)
	}
	archiveRef := stringMap(snapshotResult.Runtime, "archiveRef")

	pvcDeletion := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_PVC")
	pvcPayload := claimedSessionArchivePayload(pvcDeletion)
	pvcPayload.PVCName = stringMap(pvcDeletion, "pvcName")
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionPVCDeletion, Principal: pvcPrincipal,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-pvc-complete"}, Payload: pvcPayload}); err != nil {
		t.Fatalf("complete archived session PVC deletion: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_turns SET state = 'QUEUED', completed_at = NULL WHERE id = (SELECT id FROM control_plane.session_turns WHERE session_id = (SELECT id FROM control_plane.sessions WHERE ref = $1) ORDER BY created_at DESC LIMIT 1)", launched.Run.SessionRef); err != nil {
		t.Fatalf("queue restore fixture turn: %v", err)
	}

	restore := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "RESTORE")
	archive, ok := restore["archive"].(map[string]any)
	if !ok {
		t.Fatalf("restore archive binding is missing: %#v", restore)
	}
	restorePayload := claimedSessionArchivePayload(restore)
	restorePayload.FormatVersion = uint32(archive["formatVersion"].(int32))
	restorePayload.ObjectKey = stringMap(archive, "objectKey")
	restorePayload.ObjectVersion = stringMap(archive, "objectVersion")
	restorePayload.ObjectETag = stringMap(archive, "objectETag")
	restorePayload.ObjectDigest = stringMap(archive, "objectDigest")
	restorePayload.ObjectSizeBytes = archive["objectSizeBytes"].(int64)
	restorePayload.RestoredSourceSHA256 = stringMap(archive, "sourceSHA256")
	restorePayload.SourceSizeBytes = archive["sourceSizeBytes"].(int64)
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionRestore, Principal: restorePrincipal,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-restore-complete"}, Payload: restorePayload}); err != nil {
		t.Fatalf("complete session restore: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_turns SET state = 'COMPLETED', completed_at = clock_timestamp() WHERE session_id = (SELECT id FROM control_plane.sessions WHERE ref = $1)", launched.Run.SessionRef); err != nil {
		t.Fatalf("complete restore fixture turn: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_storage SET idle_since = clock_timestamp() WHERE session_id = (SELECT id FROM control_plane.sessions WHERE ref = $1)", launched.Run.SessionRef); err != nil {
		t.Fatalf("make restored session ineligible for snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.session_archives SET retention_until = clock_timestamp() - interval '1 second' WHERE ref = $1", archiveRef); err != nil {
		t.Fatalf("make superseded session archive eligible for GC: %v", err)
	}

	objectDeletion := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_OBJECT")
	objectPayload := claimedSessionArchivePayload(objectDeletion)
	objectPayload.ObjectKey = stringMap(objectDeletion, "objectKey")
	objectPayload.ObjectVersion = stringMap(objectDeletion, "objectVersion")
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionObjectDeletion, Principal: objectPrincipal,
		Mutation: value.Mutation{IdempotencyKey: "session-archive-object-complete"}, Payload: objectPayload}); err != nil {
		t.Fatalf("complete session archive object deletion: %v", err)
	}
	var lifecycle, storageState string
	if err := pool.QueryRow(ctx, "SELECT archive.lifecycle_state, storage.state FROM control_plane.session_archives archive JOIN control_plane.session_storage storage ON storage.session_id = archive.session_id WHERE archive.ref = $1", archiveRef).Scan(&lifecycle, &storageState); err != nil {
		t.Fatalf("read session archive lifecycle: %v", err)
	}
	if lifecycle != "DELETED" || storageState != "LIVE" {
		t.Fatalf("unexpected final session archive lifecycle: archive=%s storage=%s", lifecycle, storageState)
	}
	testMissingSessionPVCTerminatesSnapshotLifecycle(t, ctx, service, pool, claimPrincipal, failPrincipal, launched.Run.SessionRef)
}

func testMissingSessionPVCTerminatesSnapshotLifecycle(t *testing.T, ctx context.Context, service *platformservice.Service,
	pool *pgxpool.Pool, claimPrincipal, failPrincipal value.Principal, sessionRef string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE control_plane.session_storage
		SET state = 'LIVE', idle_since = clock_timestamp() - interval '1 hour'
		WHERE session_id = (SELECT id FROM control_plane.sessions WHERE ref = $1)`, sessionRef); err != nil {
		t.Fatalf("make missing PVC fixture eligible for snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, querySessionArchiveMaterializeTasks, pgx.StrictNamedArgs{
		"idle_seconds": int64(sessionArchiveIdleAfter.Seconds()), "retention_seconds": int64(sessionArchiveRetention.Seconds()),
		"maximum_attempts": sessionArchiveMaxAttempts,
	}); err != nil {
		t.Fatalf("materialize missing PVC snapshot fixture: %v", err)
	}

	var taskRef string
	for attempt := int32(1); attempt <= sessionArchiveMaxAttempts; attempt++ {
		snapshot := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
		if taskRef == "" {
			taskRef = stringMap(snapshot, "taskRef")
		} else if stringMap(snapshot, "taskRef") != taskRef {
			t.Fatalf("missing PVC retry changed task identity: got=%s want=%s", stringMap(snapshot, "taskRef"), taskRef)
		}
		result, err := service.Execute(ctx, command.Command{Kind: command.FailSessionArchiveTask, Principal: failPrincipal,
			Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("session-archive-missing-pvc-%d", attempt)},
			Payload: func() command.SessionArchiveTaskInput {
				payload := claimedSessionArchivePayload(snapshot)
				payload.SafeErrorCode = "SESSION_ARCHIVE_PVC_MISSING"
				return payload
			}(),
		})
		if err != nil {
			t.Fatalf("fail missing PVC snapshot attempt %d: %v", attempt, err)
		}
		expectedState := "READY"
		if attempt == sessionArchiveMaxAttempts {
			expectedState = "DEAD_LETTER"
		}
		if stringMap(result.Runtime, "state") != expectedState {
			t.Fatalf("missing PVC snapshot attempt %d state=%s want=%s", attempt, stringMap(result.Runtime, "state"), expectedState)
		}
		if expectedState == "READY" {
			if _, err := pool.Exec(ctx, "UPDATE control_plane.session_archive_tasks SET available_at = clock_timestamp() - interval '1 second' WHERE ref = $1", taskRef); err != nil {
				t.Fatalf("make missing PVC retry available: %v", err)
			}
		}
	}

	var taskState, safeErrorCode, storageState string
	if err := pool.QueryRow(ctx, `SELECT task.state, task.safe_error_code, storage.state
		FROM control_plane.session_archive_tasks task
		JOIN control_plane.session_storage storage ON storage.session_id = task.session_id
		WHERE task.ref = $1`, taskRef).Scan(&taskState, &safeErrorCode, &storageState); err != nil {
		t.Fatalf("read missing PVC terminal lifecycle: %v", err)
	}
	if taskState != "DEAD_LETTER" || safeErrorCode != "SESSION_ARCHIVE_PVC_MISSING" || storageState != "ERROR" {
		t.Fatalf("missing PVC terminal lifecycle: task=%s error=%s storage=%s", taskState, safeErrorCode, storageState)
	}
	if _, err := pool.Exec(ctx, querySessionArchiveMaterializeTasks, pgx.StrictNamedArgs{
		"idle_seconds": int64(sessionArchiveIdleAfter.Seconds()), "retention_seconds": int64(sessionArchiveRetention.Seconds()),
		"maximum_attempts": sessionArchiveMaxAttempts,
	}); err != nil {
		t.Fatalf("rematerialize after missing PVC terminal state: %v", err)
	}
	var replacementTasks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.session_archive_tasks task
		JOIN control_plane.sessions session ON session.id = task.session_id
		WHERE session.ref = $1 AND task.kind = 'SNAPSHOT' AND task.ref <> $2
		  AND task.state IN ('READY', 'CLAIMED')`, sessionRef, taskRef).Scan(&replacementTasks); err != nil {
		t.Fatalf("read replacement snapshot tasks: %v", err)
	}
	if replacementTasks != 0 {
		t.Fatalf("missing PVC terminal state rematerialized %d replacement snapshot tasks", replacementTasks)
	}
}

func sessionArchivePrincipal(t *testing.T, ctx context.Context, repository *Repository, operation string) value.Principal {
	t.Helper()
	return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "session-archive", Operation: operation,
	}, "session-archive")
}

func claimSingleSessionArchiveTask(t *testing.T, ctx context.Context, service *platformservice.Service, principal value.Principal, kind string) map[string]any {
	t.Helper()
	claimed, err := service.ClaimSessionArchiveTasks(ctx, principal, "session-archive-component", 1)
	if err != nil || len(claimed) != 1 || stringMap(claimed[0], "kind") != kind {
		t.Fatalf("claim %s session archive task: claims=%#v err=%v", kind, claimed, err)
	}
	return claimed[0]
}

func claimedSessionArchivePayload(task map[string]any) command.SessionArchiveTaskInput {
	return command.SessionArchiveTaskInput{TaskRef: stringMap(task, "taskRef"), LeaseRef: stringMap(task, "leaseRef"),
		Fence: stringMap(task, "fence"), Generation: task["generation"].(int64)}
}
