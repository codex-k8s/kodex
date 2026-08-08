INSERT INTO integration_gateway.integration_test_receipts (
    test_id, tenant_id, project_id, connection_id, connection_version,
    connection_generation, definition_id, definition_version, definition_sha256,
    configuration_id, configuration_version, configuration_sha256,
    credential_generation, credential_binding_id, credential_binding_version,
    credential_binding_sha256, category,
    receipt_sha256, expires_at, created_at
) VALUES (
    @test_id, @tenant_id, @project_id, @connection_id, @connection_version,
    @connection_generation, @definition_id, @definition_version, @definition_sha256,
    @configuration_id, @configuration_version, @configuration_sha256,
    @credential_generation, @credential_binding_id, @credential_binding_version,
    @credential_binding_sha256, @category,
    @receipt_sha256, @expires_at, @created_at
)
