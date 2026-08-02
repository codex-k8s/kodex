-- name: ReceiptInsert
INSERT INTO integration_gateway.idempotency_receipts (
    tenant_id, project_id, key_hash, request_hash, invocation_id, created_at
) VALUES (
    @tenant_id, @project_id, @key_hash, @request_hash, @invocation_id, @created_at
)
