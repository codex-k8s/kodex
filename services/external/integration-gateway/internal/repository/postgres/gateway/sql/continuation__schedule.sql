-- name: ContinuationSchedule
UPDATE integration_gateway.continuation_effects SET
    desired_action = @desired_action,
    action = CASE WHEN action = 'NONE' THEN @desired_action ELSE action END,
    available_at = least(available_at, @available_at),
    updated_at = @updated_at
 WHERE invocation_id = @invocation_id
   AND tenant_id = @tenant_id AND project_id = @project_id
RETURNING invocation_id
