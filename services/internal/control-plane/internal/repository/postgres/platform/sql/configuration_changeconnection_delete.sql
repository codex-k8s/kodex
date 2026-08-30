-- name: configuration_changeconnection_delete :one
UPDATE control_plane.integration_connections
SET lifecycle_state = 'DELETED',
    state = 'DISABLED',
    enabled = false,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @connection_id::uuid
  AND version = @expected_version
  AND lifecycle_state = 'ACTIVE'
  AND NOT enabled
  AND state = 'DISABLED'
RETURNING lifecycle_state, version, updated_at
