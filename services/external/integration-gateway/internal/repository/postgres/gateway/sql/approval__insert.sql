-- name: ApprovalInsert
INSERT INTO integration_gateway.approvals (
    approval_id, tenant_id, project_id, invocation_id, request_hash,
    status, expires_at, payload, created_at, decided_at
) VALUES (
    @approval_id, @tenant_id, @project_id, @invocation_id, @request_hash,
    @status, @expires_at, @payload::jsonb, @created_at, @decided_at
)
