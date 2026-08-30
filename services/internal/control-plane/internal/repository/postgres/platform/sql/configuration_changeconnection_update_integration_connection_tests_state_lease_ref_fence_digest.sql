-- name: configuration_changeconnection_update_integration_connection_tests_state_lease_ref_fence_digest :exec
UPDATE control_plane.integration_connection_tests
SET state = 'CANCELLED',
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    completed_at = clock_timestamp(),
    version = version + 1,
    updated_at = clock_timestamp()
WHERE connection_id = (
    SELECT id
    FROM control_plane.integration_connections
    WHERE organization_id = @organization_id::uuid
      AND ref = @connection_ref
)
  AND state IN ('DUE', 'CLAIMED')
