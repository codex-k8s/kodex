-- name: AttemptInsert
INSERT INTO integration_gateway.execution_attempts (
    attempt_id, tenant_id, project_id, invocation_id, attempt_number, fence,
    connection_generation, grant_generation, provider_idempotency_key,
    outcome, payload, started_at
) VALUES (
    @attempt_id, @tenant_id, @project_id, @invocation_id, @attempt_number, @fence,
    @connection_generation, @grant_generation, @provider_idempotency_key,
    @outcome, @payload::jsonb, @started_at
)
