-- name: secret_draft_operation_active :one
SELECT EXISTS(SELECT 1 FROM control_plane.runtime_secret_draft_operations
WHERE draft_id=@draft_id::uuid AND state IN ('PREPARED','CLAIMED'));
