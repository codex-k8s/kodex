SELECT connection.payload
  FROM integration_gateway.managed_provider_connections AS connection
 WHERE connection.connection_id = @resource_id
   AND connection.tenant_id = @tenant_id AND connection.project_id = @project_id
