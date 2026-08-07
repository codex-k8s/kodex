UPDATE integration_gateway.managed_provider_pools
   SET display_name = @display_name, policy = @policy, version = @version,
       desired_sha256 = @desired_sha256, observation_version = @observation_version,
       observation_sha256 = @observation_sha256, effective_sha256 = @effective_sha256,
       status = @status, payload = @payload::jsonb, updated_at = @updated_at
 WHERE provider_pool_id = @provider_pool_id AND version = @expected_version
RETURNING provider_pool_id
