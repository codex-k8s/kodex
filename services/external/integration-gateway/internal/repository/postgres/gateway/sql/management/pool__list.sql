SELECT payload
  FROM integration_gateway.managed_provider_pools
 WHERE tenant_id = @tenant_id AND project_id = @project_id AND provider_pool_id > @after_id
 ORDER BY provider_pool_id
 LIMIT @page_limit
