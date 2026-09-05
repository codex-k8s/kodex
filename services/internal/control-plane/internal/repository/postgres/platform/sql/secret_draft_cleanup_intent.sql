-- name: secret_draft_cleanup_intent :exec
UPDATE control_plane.runtime_secret_draft_operations
SET encrypted_cleanup_descriptor=COALESCE(encrypted_cleanup_descriptor,@encrypted::jsonb),
materialization_cleanup_descriptor=COALESCE(materialization_cleanup_descriptor,@materialization::jsonb),
cleanup_completed=false,
updated_at=clock_timestamp() WHERE id=@operation_id::uuid;
