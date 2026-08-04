-- name: InvocationInsert
INSERT INTO integration_gateway.invocations (
    invocation_id, tenant_id, project_id, transport_session_id,
    agent_session_id, turn_id, attempt, connection_id,
    connection_generation, grant_id, grant_generation, semantic_key,
    canonical_request_hash, status, expires_at, payload, created_at, updated_at
) VALUES (
    @invocation_id, @tenant_id, @project_id, @transport_session_id,
    @agent_session_id, @turn_id, @attempt, @connection_id,
    @connection_generation, @grant_id, @grant_generation, @semantic_key,
    @canonical_request_hash, @status, @expires_at, @payload::jsonb, @created_at, @updated_at
)
