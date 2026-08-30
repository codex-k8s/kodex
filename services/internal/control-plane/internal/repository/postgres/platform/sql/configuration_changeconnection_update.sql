-- name: configuration_changeconnection_update :one
UPDATE control_plane.integration_connections
SET name = @name,
    public_configuration = @public_configuration::jsonb,
    state = CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,
    last_test_summary = '',
    last_tested_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @connection_id::uuid
  AND version = @expected_version
  AND lifecycle_state = 'ACTIVE'
RETURNING ref
