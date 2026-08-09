-- name: operation__defer :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5
UPDATE interaction_gateway_agent_bot_operations
SET state = CASE WHEN recovery_deadline <= clock_timestamp() THEN 'REPAIR_REQUIRED' ELSE 'AMBIGUOUS' END,
    failure_code = CASE WHEN recovery_deadline <= clock_timestamp() THEN 'RECOVERY_TIMEOUT' ELSE @arg2::text END,
    retry_not_before = clock_timestamp() + @arg3::interval,
    lease_owner = '', lease_token_sha256 = repeat('0', 64), lease_expires_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND fence = @arg4::bigint AND lease_token_sha256 = @arg5::text
RETURNING state, failure_code, retry_not_before, recovery_deadline, updated_at;
