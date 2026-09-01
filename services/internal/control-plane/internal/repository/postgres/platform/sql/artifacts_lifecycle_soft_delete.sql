-- name: artifacts_lifecycle_soft_delete :exec
UPDATE control_plane.artifacts
SET lifecycle_state = 'DELETED',
    deleted_at = clock_timestamp(),
    purge_after = clock_timestamp() + interval '30 days',
    purged_at = NULL,
    version = version + 1
WHERE id = @artifact_id::uuid
  AND version = @expected_version
  AND lifecycle_state = 'ACTIVE';
