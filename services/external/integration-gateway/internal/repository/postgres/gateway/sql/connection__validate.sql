-- name: ConnectionValidate
UPDATE integration_gateway.connections SET
    status = @status, payload = @payload::jsonb, updated_at = @updated_at
 WHERE connection_id = @connection_id
   AND tenant_id = @tenant_id AND project_id = @project_id
   AND generation = @expected_generation AND status <> 'REVOKED'
RETURNING connection_id
