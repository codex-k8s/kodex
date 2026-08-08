SELECT payload, clock_timestamp()
  FROM integration_gateway.git_source_bindings
 WHERE binding_id = @binding_id AND tenant_id = @tenant_id AND project_id = @project_id
 FOR UPDATE
