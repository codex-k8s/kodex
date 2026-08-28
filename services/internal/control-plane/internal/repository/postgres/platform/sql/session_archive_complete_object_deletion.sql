-- name: session_archive_complete_object_deletion :one
WITH task_completed AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = 'SUCCEEDED', workload_instance = NULL, lease_ref = NULL,
           fence_digest = NULL, lease_expires_at = NULL, completed_at = clock_timestamp(),
           updated_at = clock_timestamp()
    WHERE task.id = @task_id::uuid
      AND task.object_key = @object_key
      AND COALESCE(task.object_version, '') = @object_version
    RETURNING task.archive_id, task.session_id, task.ref
), archive_deleted AS (
    UPDATE control_plane.session_archives archive
       SET lifecycle_state = 'DELETED', deleted_at = clock_timestamp()
      FROM task_completed completed
     WHERE archive.id = completed.archive_id
       AND archive.object_key = @object_key
       AND archive.object_version = @object_version
    RETURNING archive.id, archive.session_id
), storage_purged AS (
    UPDATE control_plane.session_storage storage
       SET state = 'PURGED', version = storage.version + 1, updated_at = clock_timestamp()
      FROM archive_deleted archive
      JOIN control_plane.sessions session ON session.id = archive.session_id
     WHERE storage.session_id = archive.session_id
       AND storage.current_archive_id = archive.id
       AND session.state = 'CLOSED'
    RETURNING storage.session_id
)
SELECT ref FROM task_completed;
