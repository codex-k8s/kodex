-- name: SessionInsert
INSERT INTO integration_gateway.transport_sessions (
    transport_session_id, tenant_id, project_id, agent_session_id, turn_id, attempt,
    input_digest, runtime_revision_id, grant_generation, token_digest, status,
    request_count, concurrent_requests, expires_at, last_seen_at, payload, created_at
) VALUES (
    @transport_session_id, @tenant_id, @project_id, @agent_session_id, @turn_id, @attempt,
    @input_digest, @runtime_revision_id, @grant_generation, @token_digest, @status,
    @request_count, @concurrent_requests, @expires_at, @last_seen_at, @payload::jsonb, clock_timestamp()
)
ON CONFLICT (transport_session_id) DO UPDATE SET
    expires_at = GREATEST(integration_gateway.transport_sessions.expires_at, EXCLUDED.expires_at),
    last_seen_at = EXCLUDED.last_seen_at
WHERE integration_gateway.transport_sessions.tenant_id = EXCLUDED.tenant_id
  AND integration_gateway.transport_sessions.project_id = EXCLUDED.project_id
  AND integration_gateway.transport_sessions.agent_session_id = EXCLUDED.agent_session_id
  AND integration_gateway.transport_sessions.turn_id = EXCLUDED.turn_id
  AND integration_gateway.transport_sessions.attempt = EXCLUDED.attempt
  AND integration_gateway.transport_sessions.input_digest = EXCLUDED.input_digest
  AND integration_gateway.transport_sessions.runtime_revision_id = EXCLUDED.runtime_revision_id
  AND integration_gateway.transport_sessions.grant_generation = EXCLUDED.grant_generation
  AND integration_gateway.transport_sessions.token_digest = EXCLUDED.token_digest
  AND integration_gateway.transport_sessions.status IN ('INITIALIZING', 'ACTIVE')
RETURNING transport_session_id
