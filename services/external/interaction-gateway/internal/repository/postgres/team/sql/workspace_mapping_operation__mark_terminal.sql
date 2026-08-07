-- name: workspace_mapping_operation__mark_terminal :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11
UPDATE interaction_gateway_workspace_mapping_operations
SET state = @arg2::text,
    result_mapping_id = @arg3::uuid,
    result_mapping_version = @arg4::bigint,
    result_mapping_generation = @arg5::bigint,
    result_provider_effect_version = @arg6::bigint,
    result_provider_effect_generation = @arg7::bigint,
    result_provider_observed_at = @arg8::timestamptz,
    result_updated_at = @arg9::timestamptz,
    failure_code = '', lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND state IN ('PENDING', 'AMBIGUOUS')
  AND fence = @arg10::bigint AND lease_token_sha256 = @arg11::text;
