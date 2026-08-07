INSERT INTO integration_gateway.provider_authorization_attempts (
    authorization_id, tenant_id, project_id, connection_id, provider_id,
    attempt, version, generation, state, intent_sha256, expires_at,
    failure_category, payload, created_at, updated_at
) VALUES (
    @authorization_id, @tenant_id, @project_id, @connection_id, @provider_id,
    @attempt, @version, @generation, @state, @intent_sha256, @expires_at,
    @failure_category, @payload::jsonb, @created_at, @updated_at
)
