SELECT pool.payload
  FROM integration_gateway.managed_provider_pools AS pool
 WHERE pool.provider_pool_id = @resource_id
   AND pool.tenant_id = @tenant_id AND pool.project_id = @project_id
