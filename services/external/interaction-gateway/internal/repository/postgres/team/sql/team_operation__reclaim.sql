UPDATE interaction_gateway_team_operations
SET fence = fence + 1,
    lease_owner = $2::text,
    lease_token_sha256 = $3::text,
    lease_expires_at = clock_timestamp() + $4::interval,
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid
  AND state = 'PENDING'
  AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp());
