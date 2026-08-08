SELECT generation, authorization_id, status, secret_ref, secret_version,
       secret_content_sha256, credential_binding_id, credential_binding_version,
       credential_binding_sha256, masked_account, masked_label, observed_usage, observed_limit,
       observation_revision, observed_at, window_duration_seconds, resets_at,
       observation_expires_at, observation_sha256
  FROM integration_gateway.provider_credential_generations
 WHERE connection_id = @connection_id
   AND tenant_id = @tenant_id AND project_id = @project_id
 ORDER BY generation
