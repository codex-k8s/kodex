-- name: integration_package__bind_connection :exec
UPDATE control_plane.integration_connections
SET definition_version = $3, definition_digest = $4,
    state = CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,
    last_test_summary = '', last_tested_at = NULL,
    version = version + 1, updated_at = clock_timestamp()
WHERE organization_id = $1::uuid AND ref = $2 AND lifecycle_state = 'ACTIVE';
