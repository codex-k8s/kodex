-- name: GrantUpsert
INSERT INTO integration_gateway.grants (
    grant_id, tenant_id, project_id, session_id, turn_id, attempt, input_digest,
    runtime_revision_id, connection_id, generation, status, expires_at, payload, updated_at
) VALUES (
    @grant_id, @tenant_id, @project_id, @session_id, @turn_id, @attempt, @input_digest,
    @runtime_revision_id, @connection_id, @generation, @status, @expires_at, @payload::jsonb,
    clock_timestamp()
)
ON CONFLICT (grant_id) DO UPDATE SET
    generation = EXCLUDED.generation,
    status = EXCLUDED.status,
    expires_at = EXCLUDED.expires_at,
    payload = EXCLUDED.payload,
    updated_at = clock_timestamp()
WHERE integration_gateway.grants.tenant_id = EXCLUDED.tenant_id
  AND integration_gateway.grants.project_id = EXCLUDED.project_id
  AND integration_gateway.grants.session_id = EXCLUDED.session_id
  AND integration_gateway.grants.turn_id = EXCLUDED.turn_id
  AND integration_gateway.grants.attempt = EXCLUDED.attempt
  AND integration_gateway.grants.input_digest = EXCLUDED.input_digest
  AND integration_gateway.grants.runtime_revision_id = EXCLUDED.runtime_revision_id
  AND integration_gateway.grants.connection_id = EXCLUDED.connection_id
  AND EXCLUDED.generation >= integration_gateway.grants.generation
RETURNING grant_id
