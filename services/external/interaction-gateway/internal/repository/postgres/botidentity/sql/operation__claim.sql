-- name: operation__claim :one
-- params: @arg1,@arg2,@arg3
WITH expired AS (
    UPDATE interaction_gateway_agent_bot_operations
    SET state = 'REPAIR_REQUIRED', failure_code = 'RECOVERY_TIMEOUT',
        lease_owner = '', lease_token_sha256 = repeat('0', 64), lease_expires_at = clock_timestamp(),
        updated_at = clock_timestamp()
    WHERE state IN ('EFFECT_PENDING', 'MEMBERSHIP_PENDING', 'AMBIGUOUS', 'PROVIDER_ACCEPTED')
      AND recovery_deadline <= clock_timestamp()
    RETURNING operation_id
), candidate AS (
    SELECT operation_id FROM interaction_gateway_agent_bot_operations
    WHERE state IN ('EFFECT_PENDING', 'MEMBERSHIP_PENDING', 'AMBIGUOUS', 'PROVIDER_ACCEPTED')
      AND retry_not_before <= clock_timestamp() AND recovery_deadline > clock_timestamp()
      AND lease_expires_at <= clock_timestamp()
    ORDER BY retry_not_before, created_at, operation_id
    FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE interaction_gateway_agent_bot_operations AS operation
SET fence = operation.fence + 1, lease_owner = @arg1::text,
    lease_token_sha256 = @arg2::text, lease_expires_at = clock_timestamp() + @arg3::interval,
    updated_at = clock_timestamp()
FROM candidate
WHERE operation.operation_id = candidate.operation_id
RETURNING operation.operation_id::text;
