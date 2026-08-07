SELECT payload, clock_timestamp()
  FROM integration_gateway.integration_configurations
 WHERE configuration_id = @configuration_id AND tenant_id = @tenant_id AND project_id = @project_id
 ORDER BY version DESC
 LIMIT 1
 FOR UPDATE
