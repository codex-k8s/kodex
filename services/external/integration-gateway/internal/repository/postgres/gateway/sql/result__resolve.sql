-- name: ResultResolve
SELECT result.payload, result.delivery_version, result.delivery_fence,
       result.acknowledged_at, result.completed_at
  FROM integration_gateway.results AS result
  JOIN integration_gateway.continuation_effects AS effect
    ON effect.invocation_id = result.invocation_id
 WHERE result.invocation_id = @invocation_id
   AND result.attempt_id = @attempt_id
   AND result.tenant_id = @tenant_id
   AND result.project_id = @project_id
   AND result.status = @outcome
   AND result.payload->>'PayloadDigest' = @reference_sha256
   AND result.acknowledged_at IS NULL
   AND effect.continuation_id = @continuation_id
   AND effect.continuation_version = @continuation_version
   AND effect.continuation_fence = @continuation_fence
   AND effect.action = 'NONE'
   AND effect.continuation_state = 'READY'
 FOR SHARE OF result, effect
