-- name: ExecutionClaim
SELECT invocation.payload, connection.payload, grant.payload, definition.payload,
       invocation.status, effect.execution_state, attempt.payload
  FROM integration_gateway.execution_work_scopes AS scope
  JOIN integration_gateway.invocations AS invocation
    ON invocation.invocation_id = scope.invocation_id
  JOIN integration_gateway.continuation_effects AS effect
    ON effect.invocation_id = invocation.invocation_id
  JOIN integration_gateway.connections AS connection
    ON connection.connection_id = invocation.connection_id
   AND connection.tenant_id = invocation.tenant_id AND connection.project_id = invocation.project_id
  JOIN integration_gateway.grants AS grant
    ON grant.grant_id = invocation.grant_id
   AND grant.tenant_id = invocation.tenant_id AND grant.project_id = invocation.project_id
  JOIN integration_gateway.definitions AS definition
    ON definition.definition_id = connection.definition_id
   AND definition.definition_version = connection.definition_version
  LEFT JOIN LATERAL (
      SELECT candidate.payload
        FROM integration_gateway.execution_attempts AS candidate
       WHERE candidate.invocation_id = invocation.invocation_id
         AND candidate.finished_at IS NULL
       ORDER BY candidate.attempt_number DESC
       LIMIT 1
  ) AS attempt ON true
 WHERE scope.tenant_id = @tenant_id AND scope.project_id = @project_id
   AND invocation.tenant_id = @tenant_id AND invocation.project_id = @project_id
   AND scope.available_at <= clock_timestamp()
   AND invocation.status IN ('APPROVED', 'EXECUTING')
   AND invocation.expires_at > clock_timestamp()
   AND effect.action = 'NONE' AND effect.approval_state = 'APPROVED'
   AND effect.continuation_state = 'SUSPENDED'
   AND ((invocation.status = 'APPROVED' AND effect.execution_state = 'NOT_STARTED')
       OR (invocation.status = 'EXECUTING' AND effect.execution_state = 'EXECUTING'
           AND attempt.payload IS NOT NULL))
   AND connection.status = 'VALID' AND connection.generation = invocation.connection_generation
   AND grant.status = 'ACTIVE' AND grant.expires_at > clock_timestamp()
   AND grant.generation = invocation.grant_generation
 ORDER BY invocation.created_at, invocation.invocation_id
 LIMIT 1
 FOR UPDATE OF invocation, effect, connection, grant SKIP LOCKED
