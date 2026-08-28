-- name: runtime_completeexecution_upsert_session_storage :exec
WITH previous AS (
    SELECT current_archive_id
    FROM control_plane.session_storage
    WHERE organization_id = @organization_id::uuid
      AND session_id = @session_id::uuid
    FOR UPDATE
), superseded AS (
    UPDATE control_plane.session_archives archive
       SET lifecycle_state = 'SUPERSEDED',
           retention_until = GREATEST(archive.retention_until, clock_timestamp() + @retention_seconds * interval '1 second')
      FROM previous
     WHERE archive.id = previous.current_archive_id
       AND archive.lifecycle_state = 'AVAILABLE'
    RETURNING archive.id
)
INSERT INTO control_plane.session_storage (
    session_id, organization_id, project_id, provider_account_id, runtime_revision_id,
    codex_session_id, content_generation, state, source_relative_path, source_sha256,
    source_size_bytes, current_archive_id, idle_since
)
SELECT session.id, session.organization_id, session.project_id, session.provider_account_id,
       revision.id, @codex_session_id::uuid, 1, 'LIVE', @source_relative_path,
       @source_sha256, @source_size_bytes, NULL, clock_timestamp()
FROM control_plane.sessions session
JOIN control_plane.runtime_revisions revision
  ON revision.id = @runtime_revision_id::uuid
 AND revision.session_id = session.id
 AND revision.provider_account_id = session.provider_account_id
 AND revision.organization_id = session.organization_id
WHERE session.id = @session_id::uuid
  AND session.organization_id = @organization_id::uuid
ON CONFLICT (session_id) DO UPDATE
SET project_id = EXCLUDED.project_id,
    provider_account_id = EXCLUDED.provider_account_id,
    runtime_revision_id = EXCLUDED.runtime_revision_id,
    codex_session_id = EXCLUDED.codex_session_id,
    content_generation = control_plane.session_storage.content_generation + 1,
    state = 'LIVE',
    source_relative_path = EXCLUDED.source_relative_path,
    source_sha256 = EXCLUDED.source_sha256,
    source_size_bytes = EXCLUDED.source_size_bytes,
    current_archive_id = NULL,
    idle_since = clock_timestamp(),
    version = control_plane.session_storage.version + 1,
    updated_at = clock_timestamp();
