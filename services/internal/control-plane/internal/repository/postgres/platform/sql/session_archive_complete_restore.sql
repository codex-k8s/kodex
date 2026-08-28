-- name: session_archive_complete_restore :one
WITH task_completed AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = 'SUCCEEDED', workload_instance = NULL, lease_ref = NULL,
           fence_digest = NULL, lease_expires_at = NULL, completed_at = clock_timestamp(),
           updated_at = clock_timestamp()
    WHERE task.id = @task_id::uuid
    RETURNING task.archive_id, task.session_id
), archive_updated AS (
    UPDATE control_plane.session_archives archive
       SET lifecycle_state = 'SUPERSEDED',
           retention_until = GREATEST(archive.retention_until, clock_timestamp() + @retention_seconds * interval '1 second')
      FROM task_completed completed
     WHERE archive.id = completed.archive_id
       AND archive.object_key = @object_key
       AND archive.object_version = @object_version
       AND archive.object_etag = @object_etag
       AND archive.object_digest = @object_digest
       AND archive.object_size_bytes = @object_size_bytes
       AND archive.format_version = @format_version
       AND archive.source_sha256 = @source_sha256
       AND archive.source_size_bytes = @source_size_bytes
    RETURNING archive.id, archive.ref, archive.session_id
), storage_updated AS (
    UPDATE control_plane.session_storage storage
       SET state = 'LIVE', current_archive_id = NULL,
           version = storage.version + 1, updated_at = clock_timestamp()
      FROM archive_updated archive
     WHERE storage.session_id = archive.session_id
       AND storage.current_archive_id = archive.id
       AND storage.state = 'RESTORING'
    RETURNING storage.session_id
)
SELECT ref FROM archive_updated
WHERE EXISTS (SELECT 1 FROM storage_updated);
