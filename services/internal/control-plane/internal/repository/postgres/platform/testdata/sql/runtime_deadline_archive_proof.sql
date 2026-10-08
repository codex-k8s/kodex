-- name: runtime_deadline_archive_proof :one
SELECT storage.source_sha256,storage.source_size_bytes
FROM control_plane.runs run JOIN control_plane.session_storage storage
 ON storage.session_id=run.session_id AND storage.organization_id=run.organization_id
WHERE run.ref=@run_ref;
