-- name: ExecutionAttemptLock
SELECT attempt.payload
  FROM integration_gateway.execution_attempts AS attempt
  JOIN integration_gateway.invocations AS invocation
    ON invocation.invocation_id = attempt.invocation_id
   AND invocation.tenant_id = attempt.tenant_id
   AND invocation.project_id = attempt.project_id
 WHERE attempt.invocation_id = @invocation_id AND attempt.attempt_id = @attempt_id
   AND attempt.tenant_id = @tenant_id AND attempt.project_id = @project_id
   AND invocation.status = 'EXECUTING'
 FOR UPDATE OF attempt
