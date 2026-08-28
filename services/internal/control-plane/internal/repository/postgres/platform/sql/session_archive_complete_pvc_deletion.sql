-- name: session_archive_complete_pvc_deletion :one
WITH task_completed AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = 'SUCCEEDED', workload_instance = NULL, lease_ref = NULL,
           fence_digest = NULL, lease_expires_at = NULL, completed_at = clock_timestamp(),
           updated_at = clock_timestamp()
    WHERE task.id = @task_id::uuid
    RETURNING task.organization_id, task.project_id, task.session_id, task.archive_id,
              task.content_generation
), storage_updated AS (
    UPDATE control_plane.session_storage storage
       SET state = CASE WHEN @active_turn THEN 'RESTORE_READY' ELSE 'ARCHIVED' END,
           version = storage.version + 1, updated_at = clock_timestamp()
      FROM task_completed completed
     WHERE storage.session_id = completed.session_id
       AND storage.current_archive_id = completed.archive_id
       AND storage.state = 'DELETE_PVC_READY'
    RETURNING storage.session_id
), restore_prepared AS (
    SELECT gen_random_uuid() AS id, completed.*
    FROM task_completed completed
    WHERE @active_turn AND EXISTS (SELECT 1 FROM storage_updated)
), restore_inserted AS (
    INSERT INTO control_plane.session_archive_tasks (
        id, ref, organization_id, project_id, session_id, archive_id, kind, state,
        content_generation, input_digest, maximum_attempts
    )
    SELECT prepared.id, 'sat_' || prepared.id::text, prepared.organization_id,
           prepared.project_id, prepared.session_id, prepared.archive_id, 'RESTORE', 'READY',
           prepared.content_generation,
           encode(digest(prepared.archive_id::text || chr(31) || 'RESTORE', 'sha256'), 'hex'),
           @maximum_attempts
    FROM restore_prepared prepared
    ON CONFLICT DO NOTHING
    RETURNING id
)
SELECT archive.ref
FROM task_completed completed
JOIN control_plane.session_archives archive ON archive.id = completed.archive_id
WHERE EXISTS (SELECT 1 FROM storage_updated);
