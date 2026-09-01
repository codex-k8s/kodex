-- name: artifacts_lifecycle_restore :exec
UPDATE control_plane.artifacts
SET lifecycle_state = 'ACTIVE',
    deleted_at = NULL,
    purge_after = NULL,
    purged_at = NULL,
    version = version + 1
WHERE id = @artifact_id::uuid
  AND version = @expected_version
  AND lifecycle_state = 'DELETED'
  AND purge_after > clock_timestamp();
