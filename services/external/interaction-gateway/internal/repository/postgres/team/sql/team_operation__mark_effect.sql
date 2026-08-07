-- name: team_operation__mark_effect :one
-- params: @arg1,@arg2,@arg3
UPDATE interaction_gateway_team_operations
SET state = 'EFFECT_PENDING',
    effect_started_at = COALESCE(effect_started_at, clock_timestamp()),
    retry_not_before = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid
  AND state = 'PENDING'
  AND fence = @arg2::bigint
  AND lease_token_sha256 = @arg3::text
RETURNING effect_started_at;
