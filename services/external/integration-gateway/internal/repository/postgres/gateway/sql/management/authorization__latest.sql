SELECT payload, device_result_ciphertext
  FROM integration_gateway.provider_authorization_attempts
 WHERE connection_id = @connection_id AND tenant_id = @tenant_id AND project_id = @project_id
 ORDER BY attempt DESC
 LIMIT 1
