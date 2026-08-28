-- name: session_archive_materialize_tasks :exec
WITH expired_locked AS MATERIALIZED (
    SELECT task.id, task.session_id, task.kind, task.object_key, task.object_version,
           task.content_generation, task.attempt, task.maximum_attempts, task.organization_id,
           task.project_id, task.archive_id
    FROM control_plane.session_archive_tasks task
    WHERE task.state = 'CLAIMED'
      AND task.lease_expires_at <= clock_timestamp()
    FOR UPDATE SKIP LOCKED
), cleanup_prepared AS (
    SELECT gen_random_uuid() AS cleanup_task_id, expired.*
    FROM expired_locked expired
    WHERE expired.kind = 'SNAPSHOT' AND expired.object_key IS NOT NULL
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
), expired_updated AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = CASE WHEN task.attempt >= task.maximum_attempts THEN 'DEAD_LETTER' ELSE 'READY' END,
           available_at = CASE WHEN task.attempt >= task.maximum_attempts THEN task.available_at
                               ELSE clock_timestamp() + LEAST(300, 5 * power(2, task.attempt)) * interval '1 second' END,
           workload_instance = NULL, lease_ref = NULL, fence_digest = NULL,
           lease_expires_at = NULL, safe_error_code = 'SESSION_ARCHIVE_LEASE_EXPIRED',
           completed_at = CASE WHEN task.attempt >= task.maximum_attempts THEN clock_timestamp() ELSE NULL END,
           updated_at = clock_timestamp()
      FROM expired_locked expired
     WHERE task.id = expired.id
    RETURNING task.session_id, task.kind, task.state
), expired_storage AS (
    UPDATE control_plane.session_storage storage
       SET state = CASE
             WHEN expired.state = 'DEAD_LETTER' AND expired.kind = 'SNAPSHOT' THEN 'LIVE'
             WHEN expired.state = 'DEAD_LETTER' THEN 'ERROR'
             WHEN expired.kind = 'SNAPSHOT' THEN 'SNAPSHOT_READY'
             WHEN expired.kind = 'RESTORE' THEN 'RESTORE_READY'
             WHEN expired.kind = 'DELETE_PVC' THEN 'DELETE_PVC_READY'
             ELSE storage.state
           END,
           version = storage.version + 1,
           updated_at = clock_timestamp()
      FROM expired_updated expired
     WHERE storage.session_id = expired.session_id
       AND expired.kind <> 'DELETE_OBJECT'
    RETURNING storage.session_id
), cancelled_deletes AS (
    UPDATE control_plane.session_archive_tasks task
       SET state = 'CANCELLED', safe_error_code = 'SESSION_BECAME_ACTIVE',
           completed_at = clock_timestamp(), updated_at = clock_timestamp()
      FROM control_plane.session_storage storage
     WHERE task.session_id = storage.session_id
       AND task.kind = 'DELETE_PVC' AND task.state = 'READY'
       AND storage.state = 'DELETE_PVC_READY'
       AND EXISTS (
           SELECT 1 FROM control_plane.session_turns turn
           WHERE turn.session_id = storage.session_id AND turn.state IN ('QUEUED', 'RUNNING')
       )
    RETURNING storage.session_id, storage.current_archive_id
), cancelled_archives AS (
    UPDATE control_plane.session_archives archive
       SET lifecycle_state = 'SUPERSEDED',
           retention_until = GREATEST(archive.retention_until, clock_timestamp() + @retention_seconds * interval '1 second')
      FROM cancelled_deletes cancelled
     WHERE archive.id = cancelled.current_archive_id
    RETURNING archive.id
), cancelled_storage AS (
    UPDATE control_plane.session_storage storage
       SET state = 'LIVE', current_archive_id = NULL,
           version = storage.version + 1, updated_at = clock_timestamp()
      FROM cancelled_deletes cancelled
     WHERE storage.session_id = cancelled.session_id
    RETURNING storage.session_id
), snapshot_candidates AS MATERIALIZED (
    SELECT storage.session_id, storage.organization_id, storage.project_id,
           storage.content_generation, organization.ref AS organization_ref,
           COALESCE(project.ref, '_system') AS project_ref, session.ref AS session_ref
    FROM control_plane.session_storage storage
    JOIN control_plane.sessions session ON session.id = storage.session_id
    JOIN control_plane.organizations organization ON organization.id = storage.organization_id
    LEFT JOIN control_plane.projects project ON project.id = storage.project_id
    WHERE storage.state = 'LIVE'
      AND session.state IN ('ACTIVE', 'CLOSED')
      AND (
          session.state = 'CLOSED'
          OR storage.idle_since <= clock_timestamp() - @idle_seconds * interval '1 second'
      )
      AND NOT EXISTS (
          SELECT 1 FROM control_plane.session_turns turn
          WHERE turn.session_id = storage.session_id AND turn.state IN ('QUEUED', 'RUNNING')
      )
      AND NOT EXISTS (
          SELECT 1 FROM control_plane.runtime_leases lease
          JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
          WHERE revision.session_id = storage.session_id AND lease.state = 'CLAIMED'
            AND lease.expires_at > clock_timestamp()
      )
    FOR UPDATE OF storage SKIP LOCKED
), snapshot_prepared AS (
    SELECT gen_random_uuid() AS id, candidate.* FROM snapshot_candidates candidate
), snapshots_inserted AS (
    INSERT INTO control_plane.session_archive_tasks (
        id, ref, organization_id, project_id, session_id, kind, state,
        content_generation, input_digest, object_key, maximum_attempts
    )
    SELECT prepared.id, 'sat_' || prepared.id::text, prepared.organization_id,
           prepared.project_id, prepared.session_id, 'SNAPSHOT', 'READY',
           prepared.content_generation,
           encode(digest(prepared.session_ref || chr(31) || prepared.content_generation::text, 'sha256'), 'hex'),
           'session-archive/v1/' || prepared.organization_ref || '/' || prepared.project_ref || '/' ||
           prepared.session_ref || '/g' || prepared.content_generation::text || '/sat_' || prepared.id::text || '-a0.tar',
           @maximum_attempts
    FROM snapshot_prepared prepared
    ON CONFLICT DO NOTHING
    RETURNING session_id
), snapshots_marked AS (
    UPDATE control_plane.session_storage storage
       SET state = 'SNAPSHOT_READY', version = storage.version + 1, updated_at = clock_timestamp()
      FROM snapshots_inserted inserted
     WHERE storage.session_id = inserted.session_id
    RETURNING storage.session_id
), restore_candidates AS MATERIALIZED (
    SELECT storage.session_id, storage.organization_id, storage.project_id,
           storage.current_archive_id, storage.content_generation, archive.object_key,
           archive.object_version
    FROM control_plane.session_storage storage
    JOIN control_plane.session_archives archive ON archive.id = storage.current_archive_id
    WHERE storage.state = 'ARCHIVED'
      AND archive.lifecycle_state = 'AVAILABLE'
      AND EXISTS (
          SELECT 1 FROM control_plane.session_turns turn
          WHERE turn.session_id = storage.session_id AND turn.state IN ('QUEUED', 'RUNNING')
      )
    FOR UPDATE OF storage SKIP LOCKED
), restore_prepared AS (
    SELECT gen_random_uuid() AS id, candidate.* FROM restore_candidates candidate
), restores_inserted AS (
    INSERT INTO control_plane.session_archive_tasks (
        id, ref, organization_id, project_id, session_id, archive_id, kind, state,
        content_generation, input_digest, maximum_attempts
    )
    SELECT prepared.id, 'sat_' || prepared.id::text, prepared.organization_id,
           prepared.project_id, prepared.session_id, prepared.current_archive_id,
           'RESTORE', 'READY', prepared.content_generation,
           encode(digest(prepared.object_key || chr(31) || prepared.object_version, 'sha256'), 'hex'),
           @maximum_attempts
    FROM restore_prepared prepared
    ON CONFLICT DO NOTHING
    RETURNING session_id
), restores_marked AS (
    UPDATE control_plane.session_storage storage
       SET state = 'RESTORE_READY', version = storage.version + 1, updated_at = clock_timestamp()
      FROM restores_inserted inserted
     WHERE storage.session_id = inserted.session_id
    RETURNING storage.session_id
), gc_candidates AS MATERIALIZED (
    SELECT archive.*
    FROM control_plane.session_archives archive
    JOIN control_plane.sessions session ON session.id = archive.session_id
    WHERE archive.retention_until <= clock_timestamp()
      AND archive.lifecycle_state IN ('SUPERSEDED', 'AVAILABLE')
      AND (archive.lifecycle_state = 'SUPERSEDED' OR session.state = 'CLOSED')
      AND NOT EXISTS (
          SELECT 1 FROM control_plane.session_archive_tasks task
          WHERE task.object_key = archive.object_key AND task.kind = 'DELETE_OBJECT'
            AND task.state IN ('READY', 'CLAIMED')
      )
    FOR UPDATE OF archive SKIP LOCKED
), gc_prepared AS (
    SELECT gen_random_uuid() AS task_id, candidate.* FROM gc_candidates candidate
), gc_inserted AS (
    INSERT INTO control_plane.session_archive_tasks (
        id, ref, organization_id, project_id, session_id, archive_id, kind, state,
        content_generation, input_digest, object_key, object_version, maximum_attempts
    )
    SELECT prepared.task_id, 'sat_' || prepared.task_id::text, prepared.organization_id,
           prepared.project_id, prepared.session_id, prepared.id, 'DELETE_OBJECT', 'READY',
           prepared.content_generation,
           encode(digest(prepared.object_key || chr(31) || prepared.object_version, 'sha256'), 'hex'),
           prepared.object_key, prepared.object_version, @maximum_attempts
    FROM gc_prepared prepared
    ON CONFLICT DO NOTHING
    RETURNING id
)
SELECT 1;
