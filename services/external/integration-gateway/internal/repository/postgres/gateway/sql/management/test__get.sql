SELECT connection_id, connection_version, connection_generation,
       definition_id, definition_version, definition_sha256,
       configuration_id, configuration_version, configuration_sha256,
       credential_generation, credential_binding_id, credential_binding_version,
       credential_binding_sha256, category, receipt_sha256, expires_at, tested_at
  FROM integration_gateway.integration_test_receipts
 WHERE test_id = @test_id AND tenant_id = @tenant_id AND project_id = @project_id
