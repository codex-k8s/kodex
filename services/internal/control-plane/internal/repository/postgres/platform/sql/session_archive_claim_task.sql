-- name: session_archive_claim_task :one
WITH claimed AS (
    UPDATE control_plane.session_archive_tasks
    SET state = 'CLAIMED', object_key = NULLIF(@object_key, ''),
        input_digest = @input_digest, attempt = attempt + 1, generation = generation + 1,
        workload_instance = @workload_instance, lease_ref = @lease_ref,
        fence_digest = @fence_digest, lease_expires_at = @lease_expires_at,
        safe_error_code = '', updated_at = clock_timestamp()
    WHERE id = @task_id::uuid AND state = 'READY'
    RETURNING session_id, kind, content_generation, attempt, generation
), storage_updated AS (
    UPDATE control_plane.session_storage storage
       SET state = CASE claimed.kind
             WHEN 'SNAPSHOT' THEN 'SNAPSHOTTING'
             WHEN 'RESTORE' THEN 'RESTORING'
             ELSE storage.state
           END,
           version = storage.version + 1,
           updated_at = clock_timestamp()
      FROM claimed
     WHERE storage.session_id = claimed.session_id
       AND storage.content_generation = claimed.content_generation
       AND (
           (claimed.kind = 'SNAPSHOT' AND storage.state = 'SNAPSHOT_READY')
           OR (claimed.kind = 'RESTORE' AND storage.state = 'RESTORE_READY')
           OR (claimed.kind = 'DELETE_PVC' AND storage.state = 'DELETE_PVC_READY')
       )
    RETURNING storage.session_id
)
SELECT attempt, generation
FROM claimed
WHERE kind = 'DELETE_OBJECT' OR EXISTS (SELECT 1 FROM storage_updated);
