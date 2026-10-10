-- name: artifacts_purge_mark_pending :exec
UPDATE control_plane.artifact_heads
SET lifecycle_state = 'PURGE_PENDING',
    version = version + 1
WHERE id = @artifact_id::uuid
  AND version = @expected_version
  AND lifecycle_state = 'DELETED';
