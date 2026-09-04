-- name: managed_configuration_detach :one
UPDATE control_plane.managed_configuration_sets
SET managed_by = 'UI', source = 'control-center', source_revision = '', version = version + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND ref = @configuration_ref
  AND managed_by = 'GIT' AND version = @expected_version
RETURNING version, updated_at;
