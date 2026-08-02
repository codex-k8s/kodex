-- name: ContinuationComplete
UPDATE integration_gateway.continuation_effects SET
    continuation_id = @continuation_id,
    continuation_version = @continuation_version,
    continuation_fence = @continuation_fence,
    approval_state = @approval_state,
    execution_state = @execution_state,
    continuation_state = @continuation_state,
    action = CASE WHEN desired_action = @action THEN 'NONE' ELSE desired_action END,
    lease_id = '', lease_expires_at = NULL,
    attempts = 0,
    available_at = clock_timestamp(),
    updated_at = clock_timestamp()
 WHERE invocation_id = @invocation_id
   AND action = @action AND lease_id = @lease_id AND lease_fence = @lease_fence
RETURNING invocation_id
