-- name: operation__repair :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_agent_bot_operations
SET state = 'REPAIR_REQUIRED', failure_code = @arg2::text,
    lease_owner = '', lease_token_sha256 = repeat('0', 64), lease_expires_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND state NOT IN ('BOUND', 'REVOKED', 'REPAIR_REQUIRED')
  AND fence = @arg3::bigint AND lease_token_sha256 = @arg4::text
  AND lease_expires_at > clock_timestamp();
