SELECT payload
  FROM integration_gateway.integration_configurations
 WHERE configuration_id = @configuration_id AND version = @version
   AND tenant_id = @tenant_id AND project_id = @project_id
