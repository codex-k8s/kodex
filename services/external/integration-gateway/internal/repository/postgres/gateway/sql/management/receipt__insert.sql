INSERT INTO integration_gateway.management_idempotency_receipts (
    tenant_id, project_id, operation, key_sha256, request_sha256,
    resource_kind, resource_id, result_version, result_sha256, result_payload, created_at
) VALUES (
    @tenant_id, @project_id, @operation, @key_sha256, @request_sha256,
    @resource_kind, @resource_id, @result_version, @result_sha256, @result_payload::jsonb, @created_at
)
