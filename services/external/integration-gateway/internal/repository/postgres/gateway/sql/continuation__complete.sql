-- name: ContinuationComplete
UPDATE integration_gateway.continuation_effects SET
    continuation_id = @continuation_id,
    continuation_version = @continuation_version,
    continuation_fence = @continuation_fence,
    approval_state = @approval_state,
    execution_state = @execution_state,
    continuation_state = @continuation_state,
	application_grant_expires_at = CASE WHEN @transition_grant_expires_at IS NULL
		THEN application_grant_expires_at ELSE @transition_grant_expires_at END,
	payload = CASE WHEN @continuation_payload::jsonb = '{}'::jsonb THEN payload ELSE @continuation_payload::jsonb END,
    action = CASE WHEN desired_action = @action THEN 'NONE' ELSE desired_action END,
    lease_id = '', lease_expires_at = NULL,
    attempts = 0,
    available_at = clock_timestamp(),
    updated_at = clock_timestamp()
 WHERE invocation_id = @invocation_id
   AND action = @action AND lease_id = @lease_id AND lease_fence = @lease_fence
RETURNING invocation_id
