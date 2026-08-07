-- name: team_operation__mark_repair :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_team_operations
SET state = 'REPAIR_REQUIRED',
    failure_code = @arg2::text,
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid
  AND state IN ('PENDING', 'EFFECT_PENDING', 'AMBIGUOUS')
  AND fence = @arg3::bigint
  AND lease_token_sha256 = @arg4::text;
