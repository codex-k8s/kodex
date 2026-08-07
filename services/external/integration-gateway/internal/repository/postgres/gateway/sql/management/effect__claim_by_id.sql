SELECT resource_id
  FROM integration_gateway.management_effects
 WHERE effect_id = @effect_id AND status = 'CLAIMED'
