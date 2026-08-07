-- name: team_operation__reclaim :exec
-- params: @arg1,@arg2,@arg3,@arg4
UPDATE interaction_gateway_team_operations
SET fence = fence + 1,
    lease_owner = @arg2::text,
    lease_token_sha256 = @arg3::text,
    lease_expires_at = clock_timestamp() + @arg4::interval,
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid
  AND state = 'PENDING'
  AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp());
