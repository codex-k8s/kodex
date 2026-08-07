SELECT status = 'SUCCEEDED'
  FROM integration_gateway.management_effects
 WHERE effect_id = @effect_id AND tenant_id = @tenant_id AND project_id = @project_id
