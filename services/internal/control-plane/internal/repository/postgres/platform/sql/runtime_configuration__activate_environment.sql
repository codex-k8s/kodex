-- name: runtime_configuration__activate_environment :exec
UPDATE control_plane.runtime_environment_sets
SET current_version_id = $2::uuid,
    updated_at = clock_timestamp()
WHERE id = $1::uuid
  AND current_version_id IS NULL;
