SELECT payload
  FROM integration_gateway.managed_provider_pools
 WHERE provider_pool_id = @provider_pool_id AND tenant_id = @tenant_id AND project_id = @project_id
