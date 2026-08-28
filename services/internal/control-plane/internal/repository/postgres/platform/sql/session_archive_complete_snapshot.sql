-- name: session_archive_complete_snapshot :one
WITH archive_inserted AS (
    INSERT INTO control_plane.session_archives (
        ref, organization_id, project_id, session_id, provider_account_id,
        runtime_revision_id, codex_session_id, content_generation, format_version,
        source_relative_path, source_sha256, source_size_bytes, object_key,
        object_version, object_etag, object_digest, object_size_bytes,
        lifecycle_state, retention_until
    )
    SELECT @archive_ref, storage.organization_id, storage.project_id, storage.session_id,
           storage.provider_account_id, storage.runtime_revision_id, storage.codex_session_id,
           storage.content_generation, @format_version, storage.source_relative_path,
           storage.source_sha256, storage.source_size_bytes, @object_key, @object_version,
           @object_etag, @object_digest, @object_size_bytes,
           CASE WHEN @active_turn THEN 'SUPERSEDED' ELSE 'AVAILABLE' END,
           clock_timestamp() + @retention_seconds * interval '1 second'
    FROM control_plane.session_storage storage
    WHERE storage.session_id = @session_id::uuid
      AND storage.organization_id = @organization_id::uuid
      AND storage.state = 'SNAPSHOTTING'
      AND storage.content_generation = @content_generation
    RETURNING id, ref, organization_id, project_id, session_id, content_generation
), task_completed AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = 'SUCCEEDED', workload_instance = NULL, lease_ref = NULL,
           fence_digest = NULL, lease_expires_at = NULL, completed_at = clock_timestamp(),
           updated_at = clock_timestamp()
      FROM archive_inserted archive
     WHERE task.id = @task_id::uuid
    RETURNING archive.*
), storage_updated AS (
    UPDATE control_plane.session_storage storage
       SET state = CASE WHEN @active_turn THEN 'LIVE' ELSE 'DELETE_PVC_READY' END,
           current_archive_id = CASE WHEN @active_turn THEN NULL ELSE completed.id END,
           version = storage.version + 1, updated_at = clock_timestamp()
      FROM task_completed completed
     WHERE storage.session_id = completed.session_id
    RETURNING storage.session_id
), delete_prepared AS (
    SELECT gen_random_uuid() AS task_id, completed.id AS archive_id, completed.ref AS archive_ref,
           completed.organization_id, completed.project_id, completed.session_id,
           completed.content_generation
    FROM task_completed completed WHERE NOT @active_turn
), delete_inserted AS (
    INSERT INTO control_plane.session_archive_tasks (
        id, ref, organization_id, project_id, session_id, archive_id, kind, state,
        content_generation, input_digest, maximum_attempts
    )
    SELECT prepared.task_id, 'sat_' || prepared.task_id::text, prepared.organization_id,
           prepared.project_id, prepared.session_id, prepared.archive_id, 'DELETE_PVC', 'READY',
           prepared.content_generation,
           encode(digest(prepared.archive_ref || chr(31) || 'DELETE_PVC', 'sha256'), 'hex'),
           @maximum_attempts
    FROM delete_prepared prepared
    ON CONFLICT DO NOTHING
    RETURNING id
)
SELECT ref FROM archive_inserted;
