-- name: repair_backlog__count :one
-- params: @arg1,@arg2
SELECT count(*) FILTER (WHERE failure_code = 'RECOVERY_TIMEOUT')::bigint,
       count(*) FILTER (WHERE failure_code <> 'RECOVERY_TIMEOUT')::bigint
FROM interaction_gateway_agent_bot_operations
WHERE organization_id = @arg1::uuid
  AND project_id = @arg2::uuid
  AND state = 'REPAIR_REQUIRED';
