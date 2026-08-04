-- name: ResultInsert
INSERT INTO integration_gateway.results (
    invocation_id, tenant_id, project_id, attempt_id, status, payload, completed_at
) VALUES (
    @invocation_id, @tenant_id, @project_id, @attempt_id, @status, @payload::jsonb, @completed_at
)
ON CONFLICT (invocation_id) DO NOTHING
RETURNING invocation_id
