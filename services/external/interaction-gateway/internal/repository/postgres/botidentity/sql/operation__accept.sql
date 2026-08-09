-- name: operation__accept :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_agent_bot_operations
SET identity_ref = @arg2::uuid, state = 'PROVIDER_ACCEPTED', failure_code = '',
    retry_not_before = clock_timestamp(), updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND fence = @arg3::bigint AND lease_token_sha256 = @arg4::text
  AND state IN ('MEMBERSHIP_PENDING', 'AMBIGUOUS', 'EFFECT_PENDING')
  AND lease_expires_at > clock_timestamp();
