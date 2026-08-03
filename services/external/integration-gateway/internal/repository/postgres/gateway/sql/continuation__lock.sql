SELECT payload
  FROM integration_gateway.continuation_effects
 WHERE invocation_id = @invocation_id
 FOR UPDATE
