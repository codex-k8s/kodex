SELECT payload
  FROM integration_gateway.managed_provider_connections
 WHERE connection_id = @connection_id AND tenant_id = @tenant_id AND project_id = @project_id
