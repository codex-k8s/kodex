-- name: ExecutionLock
SELECT invocation.payload, attempt.payload, connection.status, connection.generation,
       grant.status, grant.generation
  FROM integration_gateway.invocations AS invocation
  JOIN integration_gateway.execution_attempts AS attempt
    ON attempt.attempt_id = @attempt_id AND attempt.invocation_id = invocation.invocation_id
  JOIN integration_gateway.connections AS connection ON connection.connection_id = invocation.connection_id
  JOIN integration_gateway.grants AS grant ON grant.grant_id = invocation.grant_id
 WHERE invocation.invocation_id = @invocation_id
   AND invocation.tenant_id = @tenant_id AND invocation.project_id = @project_id
 FOR UPDATE OF invocation, attempt, connection, grant
