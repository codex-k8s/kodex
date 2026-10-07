-- name: runtime_terminal_storage_fixture :exec
WITH inserted AS (
    INSERT INTO control_plane.session_archives (
        ref, organization_id, project_id, session_id, provider_account_id, runtime_revision_id,
        codex_session_id, content_generation, format_version, source_relative_path,
        source_sha256, source_size_bytes, object_key, object_version, object_etag,
        object_digest, object_size_bytes, retention_until
    )
    SELECT 'sar_fixture_' || replace(storage.session_id::text, '-', ''), storage.organization_id,
           storage.project_id, storage.session_id, storage.provider_account_id, storage.runtime_revision_id,
           storage.codex_session_id, storage.content_generation, 1, storage.source_relative_path,
           storage.source_sha256, storage.source_size_bytes, 'fixture/' || storage.session_id::text,
           '', 'fixture', 'sha256:' || repeat('a', 64), 128, clock_timestamp() + interval '1 day'
    FROM control_plane.session_storage storage JOIN control_plane.sessions session ON session.id = storage.session_id
    WHERE session.ref = @session_ref
    ON CONFLICT (ref) DO NOTHING
    RETURNING id
)
UPDATE control_plane.session_storage storage
SET state = @state, current_archive_id = CASE
      WHEN @state IN ('DELETE_PVC_READY', 'ARCHIVED', 'RESTORE_READY', 'RESTORING', 'ERROR', 'PURGED')
      THEN COALESCE((SELECT id FROM inserted), (SELECT archive.id FROM control_plane.session_archives archive
                   WHERE archive.ref = 'sar_fixture_' || replace(storage.session_id::text, '-', '')))
      ELSE NULL END,
    version = storage.version + 1, updated_at = clock_timestamp()
FROM control_plane.sessions session
WHERE session.id = storage.session_id AND session.ref = @session_ref;
