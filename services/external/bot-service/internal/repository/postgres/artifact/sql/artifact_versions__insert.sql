-- name: artifact_versions__insert :exec
insert into matter_codex_artifact_versions(
	id, artifact_id, storage_key, original_name, safe_name, media_type,
	declared_media_type, size_bytes, sha256, state, error_code, created_at, updated_at
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12);
