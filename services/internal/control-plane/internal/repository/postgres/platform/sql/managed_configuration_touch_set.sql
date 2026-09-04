-- name: managed_configuration_touch_set :one
UPDATE control_plane.managed_configuration_sets
SET version = version + 1, updated_at = clock_timestamp()
WHERE id = @configuration_set_id::uuid AND version = @expected_version
RETURNING version, updated_at;
