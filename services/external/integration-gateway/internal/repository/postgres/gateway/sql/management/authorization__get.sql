SELECT payload, device_result_ciphertext
  FROM integration_gateway.provider_authorization_attempts
 WHERE authorization_id = @authorization_id AND tenant_id = @tenant_id AND project_id = @project_id
