-- name: runtime_session_resume_snapshot :one
SELECT revision.safe_snapshot
FROM control_plane.session_storage storage
JOIN control_plane.sessions session
  ON session.id = storage.session_id AND session.organization_id = storage.organization_id
JOIN control_plane.runtime_revisions revision
  ON revision.id = storage.runtime_revision_id
 AND revision.organization_id = storage.organization_id
 AND revision.session_id = storage.session_id
 AND revision.provider_account_id = storage.provider_account_id
WHERE storage.organization_id = @organization_id::uuid
  AND session.ref = @session_ref
  AND storage.codex_session_id = @codex_session_id::uuid
  AND storage.state = 'LIVE'
  AND storage.source_size_bytes > 0
  AND storage.source_sha256 ~ '^[a-f0-9]{64}$'
FOR SHARE OF storage;
