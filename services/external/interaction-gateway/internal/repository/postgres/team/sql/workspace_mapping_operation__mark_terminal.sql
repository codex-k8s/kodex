UPDATE interaction_gateway_workspace_mapping_operations
SET state = $2::text,
    result_mapping_id = $3::uuid,
    result_mapping_version = $4::bigint,
    result_mapping_generation = $5::bigint,
    result_provider_effect_version = $6::bigint,
    result_provider_effect_generation = $7::bigint,
    result_provider_observed_at = $8::timestamptz,
    result_updated_at = $9::timestamptz,
    failure_code = '', lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = $1::uuid AND state IN ('PENDING', 'AMBIGUOUS')
  AND fence = $10::bigint AND lease_token_sha256 = $11::text;
