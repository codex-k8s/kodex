SELECT authorization_id, status, secret_ref, secret_version, secret_content_sha256,
       credential_binding_id, credential_binding_version, credential_binding_sha256,
       masked_account, masked_label
  FROM integration_gateway.provider_credential_generations
 WHERE connection_id = @connection_id AND generation = @generation
   AND tenant_id = @tenant_id AND project_id = @project_id
