-- name: operation__mark_effect :one
-- params: @arg1,@arg2,@arg3
UPDATE interaction_gateway_agent_bot_operations
SET effect_started_at = COALESCE(effect_started_at, clock_timestamp()), updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND fence = @arg2::bigint AND lease_token_sha256 = @arg3::text
  AND state = 'EFFECT_PENDING' AND lease_expires_at > clock_timestamp()
RETURNING effect_started_at, updated_at;
