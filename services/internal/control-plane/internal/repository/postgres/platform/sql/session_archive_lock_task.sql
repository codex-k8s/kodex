-- name: session_archive_lock_task :one
SELECT task.id::text, task.ref, task.kind, task.state, task.generation,
       task.fence_digest, task.lease_ref, task.lease_expires_at,
       task.attempt, task.maximum_attempts, task.organization_id::text,
       COALESCE(task.project_id::text, ''), task.session_id::text,
       COALESCE(task.archive_id::text, ''), task.content_generation,
       task.input_digest, COALESCE(task.object_key, ''), COALESCE(task.object_version, ''),
       storage.state, storage.content_generation, storage.source_relative_path,
       storage.source_sha256, storage.source_size_bytes,
       COALESCE(storage.current_archive_id::text, ''), session.ref,
       control_plane.session_archive_pending_execution(task.organization_id, task.session_id)
FROM control_plane.session_archive_tasks task
JOIN control_plane.session_storage storage ON storage.session_id = task.session_id
JOIN control_plane.sessions session ON session.id = task.session_id
WHERE task.organization_id = @organization_id::uuid AND task.ref = @task_ref
FOR UPDATE OF task, storage;
