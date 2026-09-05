-- name: secret_draft_cleanup_complete :exec
UPDATE control_plane.runtime_secret_draft_operations
SET cleanup_completed=true,updated_at=clock_timestamp() WHERE id=@operation_id::uuid;
