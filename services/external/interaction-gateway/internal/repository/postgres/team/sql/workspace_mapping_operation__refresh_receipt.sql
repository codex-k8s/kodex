UPDATE interaction_gateway_workspace_mapping_operations
SET state = 'PENDING', effect_generation = $4::bigint, receipt_id = $5::uuid,
    failure_code = '', retry_not_before = clock_timestamp(), updated_at = clock_timestamp()
WHERE operation_id = $1::uuid AND state = 'AMBIGUOUS'
  AND fence = $2::bigint AND lease_token_sha256 = $3::text
  AND effect_generation < $4::bigint;
