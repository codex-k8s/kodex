-- name: ResultResolve
SELECT result.payload, result.delivery_version, result.delivery_fence,
       result.acknowledged_at, result.completed_at
  FROM integration_gateway.results AS result
 WHERE result.invocation_id = @invocation_id
   AND result.attempt_id = @attempt_id
   AND result.tenant_id = @tenant_id
   AND result.project_id = @project_id
   AND result.status = 'SUCCEEDED'
 FOR SHARE
