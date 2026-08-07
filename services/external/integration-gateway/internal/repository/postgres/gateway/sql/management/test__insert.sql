INSERT INTO integration_gateway.integration_test_receipts (
    test_id, tenant_id, project_id, connection_id, connection_version,
    connection_generation, definition_id, definition_version, category,
    receipt_sha256, expires_at, created_at
) VALUES (
    @test_id, @tenant_id, @project_id, @connection_id, @connection_version,
    @connection_generation, @definition_id, @definition_version, @category,
    @receipt_sha256, @expires_at, @created_at
)
