UPDATE interaction_gateway_workspace_mapping_operations
SET state = 'REPAIR_REQUIRED', failure_code = $2::text,
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid AND state IN ('PENDING', 'AMBIGUOUS')
  AND fence = $3::bigint AND lease_token_sha256 = $4::text;
