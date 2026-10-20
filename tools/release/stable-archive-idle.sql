-- name: stable_archive_idle :one
-- Проверка владельца перед сменой controller/worker OCI, без payload и secrets.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object(
    'openArchiveTasks', (SELECT count(*) FROM control_plane.session_archive_tasks
        WHERE state IN ('READY', 'CLAIMED')),
    'transitionalStorage', (SELECT count(*) FROM control_plane.session_storage
        WHERE state IN ('SNAPSHOT_READY', 'SNAPSHOTTING', 'DELETE_PVC_READY',
                        'RESTORE_READY', 'RESTORING'))
)::text;
COMMIT;
