-- name: secret_draft_operation_reissue :one
UPDATE control_plane.runtime_secret_draft_operations
SET token_digest=@token_digest,grant_expires_at=@grant_expires_at,updated_at=clock_timestamp()
WHERE id=@operation_id::uuid AND state='PREPARED'
RETURNING grant_expires_at;
