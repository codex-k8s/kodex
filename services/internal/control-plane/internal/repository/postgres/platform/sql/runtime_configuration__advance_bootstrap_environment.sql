-- name: runtime_configuration__advance_bootstrap_environment :exec
UPDATE control_plane.runtime_environment_sets
SET current_version_id = @next_version_id::uuid,
    updated_at = clock_timestamp()
WHERE id = @environment_id::uuid
  AND current_version_id IS NOT DISTINCT FROM NULLIF(@current_version_id, '')::uuid;
