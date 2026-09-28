-- name: secret_draft_cleanup_intent :exec
UPDATE control_plane.runtime_secret_draft_operations
SET encrypted_cleanup_descriptor=COALESCE(encrypted_cleanup_descriptor,@encrypted::jsonb),
materialization_cleanup_descriptor=COALESCE(materialization_cleanup_descriptor,@materialization::jsonb),
cleanup_completed=CASE
  WHEN (encrypted_cleanup_descriptor IS NULL AND @encrypted::jsonb IS NOT NULL)
    OR (materialization_cleanup_descriptor IS NULL AND @materialization::jsonb IS NOT NULL)
  THEN false
  ELSE cleanup_completed
END,
updated_at=clock_timestamp() WHERE id=@operation_id::uuid;
