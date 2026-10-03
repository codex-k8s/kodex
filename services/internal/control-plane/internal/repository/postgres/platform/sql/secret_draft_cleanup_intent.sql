-- name: secret_draft_cleanup_intent :exec
UPDATE control_plane.runtime_secret_draft_operations
SET encrypted_cleanup_descriptor=CASE WHEN cleanup_completed THEN @encrypted::jsonb
  ELSE COALESCE(encrypted_cleanup_descriptor,@encrypted::jsonb) END,
materialization_cleanup_descriptor=CASE WHEN cleanup_completed THEN @materialization::jsonb
  ELSE COALESCE(materialization_cleanup_descriptor,@materialization::jsonb) END,
cleanup_completed=CASE
  WHEN (cleanup_completed AND (@encrypted::jsonb IS NOT NULL OR @materialization::jsonb IS NOT NULL))
    OR (encrypted_cleanup_descriptor IS NULL AND @encrypted::jsonb IS NOT NULL)
    OR (materialization_cleanup_descriptor IS NULL AND @materialization::jsonb IS NOT NULL)
  THEN false
  ELSE cleanup_completed
END,
updated_at=clock_timestamp() WHERE id=@operation_id::uuid;
