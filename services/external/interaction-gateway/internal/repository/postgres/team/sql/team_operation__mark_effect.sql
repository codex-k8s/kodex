UPDATE interaction_gateway_team_operations
SET state = 'EFFECT_PENDING',
    effect_started_at = COALESCE(effect_started_at, clock_timestamp()),
    retry_not_before = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid
  AND state = 'PENDING'
  AND fence = $2::bigint
  AND lease_token_sha256 = $3::text
RETURNING effect_started_at;
