SELECT DISTINCT ON (configuration_id) payload
  FROM integration_gateway.integration_configurations
 WHERE tenant_id = @tenant_id AND project_id = @project_id AND configuration_id > @after_id
 ORDER BY configuration_id, version DESC
 LIMIT @page_limit
