-- name: team_operation__mark_ambiguous :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5
UPDATE interaction_gateway_team_operations
SET state = CASE WHEN clock_timestamp() < recovery_deadline THEN 'AMBIGUOUS' ELSE 'REPAIR_REQUIRED' END,
    failure_code = CASE WHEN clock_timestamp() < recovery_deadline THEN @arg2::text ELSE 'RECOVERY_TIMEOUT' END,
    retry_not_before = LEAST(recovery_deadline, clock_timestamp() + @arg3::interval),
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid
  AND state IN ('EFFECT_PENDING', 'AMBIGUOUS')
  AND fence = @arg4::bigint
  AND lease_token_sha256 = @arg5::text
RETURNING state, failure_code, retry_not_before, recovery_deadline, updated_at;
