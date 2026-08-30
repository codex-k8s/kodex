-- name: runtime_configuration__update_environment_lifecycle :one
UPDATE control_plane.runtime_environment_sets
SET state = @state, version = version + 1, updated_at = clock_timestamp()
WHERE id = @environment_id::uuid
  AND version = @expected_version
  AND state <> 'DELETED'
RETURNING state, version, updated_at;
