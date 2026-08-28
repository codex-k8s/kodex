-- name: session_archive_fail_task :one
WITH failed AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = CASE WHEN attempt >= maximum_attempts THEN 'DEAD_LETTER' ELSE 'READY' END,
           available_at = CASE WHEN attempt >= maximum_attempts THEN available_at
                               ELSE clock_timestamp() + LEAST(300, 5 * power(2, attempt)) * interval '1 second' END,
           workload_instance = NULL, lease_ref = NULL, fence_digest = NULL,
           lease_expires_at = NULL, safe_error_code = @safe_error_code,
           completed_at = CASE WHEN attempt >= maximum_attempts THEN clock_timestamp() ELSE NULL END,
           updated_at = clock_timestamp()
     WHERE task.id = @task_id::uuid
    RETURNING task.*, state = 'READY' AS retry_scheduled
), cleanup_prepared AS (
    SELECT gen_random_uuid() AS cleanup_task_id, failed.*
    FROM failed
    WHERE failed.kind = 'SNAPSHOT' AND failed.object_key IS NOT NULL
), cleanup_inserted AS (
    INSERT INTO control_plane.session_archive_tasks (
        id, ref, organization_id, project_id, session_id, archive_id, kind, state,
        content_generation, input_digest, object_key, object_version, maximum_attempts
    )
    SELECT prepared.cleanup_task_id, 'sat_' || prepared.cleanup_task_id::text, prepared.organization_id,
           prepared.project_id, prepared.session_id, prepared.archive_id,
           'DELETE_OBJECT', 'READY', prepared.content_generation,
           encode(digest(prepared.object_key || chr(31) || COALESCE(prepared.object_version, ''), 'sha256'), 'hex'),
           prepared.object_key, prepared.object_version, @maximum_attempts
    FROM cleanup_prepared prepared
    ON CONFLICT DO NOTHING
    RETURNING id
), storage_updated AS (
    UPDATE control_plane.session_storage storage
       SET state = CASE
             WHEN failed.kind = 'SNAPSHOT' AND failed.retry_scheduled THEN 'SNAPSHOT_READY'
             WHEN failed.kind = 'SNAPSHOT' THEN 'LIVE'
             WHEN failed.kind = 'RESTORE' AND failed.retry_scheduled THEN 'RESTORE_READY'
             WHEN failed.kind = 'DELETE_PVC' AND failed.retry_scheduled THEN 'DELETE_PVC_READY'
             ELSE 'ERROR'
           END,
           version = storage.version + 1, updated_at = clock_timestamp()
      FROM failed
     WHERE storage.session_id = failed.session_id AND failed.kind <> 'DELETE_OBJECT'
    RETURNING storage.session_id
)
SELECT ref, state, retry_scheduled FROM failed;
