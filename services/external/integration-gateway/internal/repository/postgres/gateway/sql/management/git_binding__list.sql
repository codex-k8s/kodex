SELECT payload
  FROM integration_gateway.git_source_bindings
 WHERE tenant_id = @tenant_id AND project_id = @project_id AND binding_id > @after_id
 ORDER BY binding_id
 LIMIT @page_limit
