-- name: stable_archive_idle :one
-- Проверка владельца перед сменой controller/worker OCI, без payload и secrets.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object(
    'openArchiveTasks', (SELECT count(*) FROM control_plane.session_archive_tasks
        WHERE state IN ('READY', 'CLAIMED')),
    'transitionalStorage', (SELECT count(*) FROM control_plane.session_storage
        WHERE state IN ('SNAPSHOT_READY', 'SNAPSHOTTING', 'DELETE_PVC_READY',
                        'RESTORE_READY', 'RESTORING')),
    'activeRuntimeRuns', (SELECT count(*) FROM control_plane.runs
        WHERE state IN ('QUEUED','RUNNING','CANCELLING','WAITING_HUMAN')),
    'claimedRuntimeLeases', (SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
    'transitionalWorkspace', (SELECT count(*) FROM control_plane.session_workspace_storage
        WHERE state IN ('WRITER_PENDING','WRITING','QUIESCING','CHECKPOINT_PENDING','RESTORE_READY','RESTORING','DELETE_READY')
           OR (state='CHECKPOINT_READY' AND NOT writer_released)),
    'activeWorkspaceVolumeTasks', (SELECT count(*) FROM control_plane.session_workspace_volume_tasks WHERE state IN ('READY','CLAIMED'))
)::text;
COMMIT;
