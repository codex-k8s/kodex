UPDATE interaction_gateway_workspace_mapping_operations
SET fence = fence + 1,
    lease_owner = $2::text,
    lease_token_sha256 = $3::text,
    lease_expires_at = clock_timestamp() + $4::interval,
    retry_not_before = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid
  AND state IN ('PENDING', 'AMBIGUOUS')
  AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp());
