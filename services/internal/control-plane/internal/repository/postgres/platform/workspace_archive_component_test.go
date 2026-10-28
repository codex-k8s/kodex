package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// workspaceArchiveArguments resolves exact fixture scope without bypassing production session locks.
// workspaceArchiveArguments получает точную область fixture, не обходя production session locks.
func workspaceArchiveArguments(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionRef string) pgx.StrictNamedArgs {
	t.Helper()
	var organization, project, session, agent string
	if err := pool.QueryRow(ctx, `SELECT s.organization_id::text,s.project_id::text,s.id::text,a.id::text
		FROM control_plane.sessions s JOIN control_plane.agents a ON a.ref=s.target_ref
		AND a.organization_id=s.organization_id AND a.project_id=s.project_id WHERE s.ref=$1`, sessionRef).
		Scan(&organization, &project, &session, &agent); err != nil {
		t.Fatal("resolve workspace archive fixture", err)
	}
	return pgx.StrictNamedArgs{"organization_id": organization, "project_id": project, "session_id": session, "agent_id": agent}
}

// workspaceArchiveTransaction rolls every fixture mutation back with an independent cleanup context.
// workspaceArchiveTransaction откатывает каждую fixture mutation с независимым cleanup context.
func workspaceArchiveTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin workspace archive fixture", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("rollback workspace archive fixture", err)
		}
	})
	return tx
}

// testWorkspaceArchiveAdmission proves READY snapshot exclusion and both physical lock orders.
// testWorkspaceArchiveAdmission доказывает отказ при READY snapshot и оба порядка реальных блокировок.
func testWorkspaceArchiveAdmission(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionRef string) {
	t.Helper()
	args := workspaceArchiveArguments(t, ctx, pool, sessionRef)
	t.Run("ready_snapshot_blocks_workspace", func(t *testing.T) {
		tx := workspaceArchiveTransaction(t, ctx, pool)
		var id, reference, state string
		var generation int64
		if err := tx.QueryRow(ctx, queryWorkspaceStorageSession, args).Scan(&id); err != nil {
			t.Fatal("lock workspace session", err)
		}
		create := pgx.StrictNamedArgs{}
		for key, value := range args {
			create[key] = value
		}
		create["workspace_ref"] = "wks_archive_fence_fixture"
		if err := tx.QueryRow(ctx, queryWorkspaceStorageCreate, create).Scan(&reference, &generation, &state); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("workspace admitted pending legacy snapshot", err)
		}
	})
	t.Run("archive_lock_excludes_workspace", func(t *testing.T) {
		archive := workspaceArchiveTransaction(t, ctx, pool)
		rows, err := archive.Query(ctx, querySessionArchiveSelectClaimableTasks, pgx.StrictNamedArgs{"organization_id": args["organization_id"], "limit": 1})
		if err != nil {
			t.Fatal("select archive fixture", err)
		}
		found := rows.Next()
		rows.Close()
		if !found || rows.Err() != nil {
			t.Fatal("expected claimable legacy snapshot")
		}
		workspace := workspaceArchiveTransaction(t, ctx, pool)
		var id string
		err = workspace.QueryRow(ctx, queryWorkspaceStorageSession, args).Scan(&id)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "55P03" {
			t.Fatal("archive did not hold shared session boundary", err)
		}
	})
	t.Run("workspace_lock_skips_archive", func(t *testing.T) {
		workspace := workspaceArchiveTransaction(t, ctx, pool)
		var id string
		if err := workspace.QueryRow(ctx, queryWorkspaceStorageSession, args).Scan(&id); err != nil {
			t.Fatal("lock workspace fixture", err)
		}
		archive := workspaceArchiveTransaction(t, ctx, pool)
		rows, err := archive.Query(ctx, querySessionArchiveSelectClaimableTasks, pgx.StrictNamedArgs{"organization_id": args["organization_id"], "limit": 1})
		if err != nil {
			t.Fatal("select contended archive fixture", err)
		}
		found := rows.Next()
		rows.Close()
		if found || rows.Err() != nil {
			t.Fatal("archive crossed existing workspace session lock")
		}
	})
}

// testWorkspaceArchiveLegacyFence injects a mixed-profile fixture only inside a rollback transaction.
// Production guards must preserve custody even if a stale legacy task survived an interrupted upgrade.
// testWorkspaceArchiveLegacyFence создаёт смешанную fixture только внутри откатываемой транзакции.
// Production guards сохраняют custody, даже если старая задача пережила прерванное обновление.
func testWorkspaceArchiveLegacyFence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionRef string, snapshot map[string]any) {
	t.Helper()
	t.Run("workspace_blocks_stale_archive_effects", func(t *testing.T) {
		args := workspaceArchiveArguments(t, ctx, pool, sessionRef)
		tx := workspaceArchiveTransaction(t, ctx, pool)
		payload := claimedSessionArchivePayload(snapshot)
		fenceDigest := sha256.Sum256([]byte(payload.Fence))
		renewArgs := pgx.StrictNamedArgs{
			"organization_id": args["organization_id"], "task_ref": payload.TaskRef, "lease_ref": payload.LeaseRef,
			"fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": payload.Generation,
			"lease_expires_at": time.Now().Add(time.Minute),
		}
		var expiry time.Time
		if err := tx.QueryRow(ctx, querySessionArchiveRenewTask, renewArgs).Scan(&expiry); err != nil {
			t.Fatal("positive legacy renewal fixture failed", err)
		}
		var before, after int
		if err := tx.QueryRow(ctx, queryProjectPurgeActiveTasks, args["organization_id"], args["project_id"]).Scan(&before); err != nil {
			t.Fatal("read purge blockers", err)
		}
		// This is a private disposable inconsistency fixture, never a live state injection.
		// Это private disposable fixture несогласованности, не вставка состояния в живую БД.
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.session_workspace_storage
			(ref,organization_id,project_id,session_id,agent_id,state)
			VALUES('wks_archive_mixed_fixture',$1::uuid,$2::uuid,$3::uuid,$4::uuid,'EMPTY')`,
			args["organization_id"], args["project_id"], args["session_id"], args["agent_id"]); err != nil {
			t.Fatal("insert isolated mixed-profile fixture", err)
		}
		if err := tx.QueryRow(ctx, queryProjectPurgeActiveTasks, args["organization_id"], args["project_id"]).Scan(&after); err != nil || after != before+1 {
			t.Fatal("workspace did not block external purge inventory", err)
		}
		if err := tx.QueryRow(ctx, querySessionArchiveRenewTask, renewArgs).Scan(&expiry); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("legacy lease renewed across workspace boundary", err)
		}
		rows, err := tx.Query(ctx, querySessionArchiveLockTask, pgx.StrictNamedArgs{"organization_id": args["organization_id"], "task_ref": payload.TaskRef})
		if err != nil {
			t.Fatal("read legacy completion boundary", err)
		}
		found := rows.Next()
		rows.Close()
		if found || rows.Err() != nil {
			t.Fatal("legacy completion obtained workspace authority")
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.session_archive_tasks SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1`, payload.TaskRef); err != nil {
			t.Fatal("expire isolated legacy lease", err)
		}
		if _, err := tx.Exec(ctx, querySessionArchiveMaterializeTasks, pgx.StrictNamedArgs{
			"idle_seconds": int64(sessionArchiveIdleAfter.Seconds()), "retention_seconds": int64(sessionArchiveRetention.Seconds()), "maximum_attempts": sessionArchiveMaxAttempts,
		}); err != nil {
			t.Fatal("reconcile mixed-profile fixture", err)
		}
		var state string
		if err := tx.QueryRow(ctx, "SELECT state FROM control_plane.session_archive_tasks WHERE ref=$1", payload.TaskRef).Scan(&state); err != nil || state != "CLAIMED" {
			t.Fatal("unknown legacy custody was reset or retried", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.session_archive_tasks SET state='READY',lease_ref=NULL,fence_digest=NULL,
			workload_instance=NULL,lease_expires_at=NULL WHERE ref=$1`, payload.TaskRef); err != nil {
			t.Fatal("prepare stale ready fixture", err)
		}
		var taskID string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM control_plane.session_archive_tasks WHERE ref=$1", payload.TaskRef).Scan(&taskID); err != nil {
			t.Fatal("read stale task identity", err)
		}
		var attempt int32
		var generation int64
		if err := tx.QueryRow(ctx, querySessionArchiveClaimTask, pgx.StrictNamedArgs{
			"task_id": taskID, "object_key": stringMap(snapshot, "objectKey"), "input_digest": strings.Repeat("a", 64),
			"workload_instance": "workspace-fence-fixture", "lease_ref": "lea_fixture", "fence_digest": strings.Repeat("b", 64),
			"lease_expires_at": time.Now().Add(time.Minute),
		}).Scan(&attempt, &generation); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("fresh claim statement admitted workspace legacy effect", err)
		}
		if err := tx.QueryRow(ctx, "SELECT state FROM control_plane.session_archive_tasks WHERE ref=$1", payload.TaskRef).Scan(&state); err != nil || state != "READY" {
			t.Fatal("denied claim mutated task", err)
		}
	})
}
