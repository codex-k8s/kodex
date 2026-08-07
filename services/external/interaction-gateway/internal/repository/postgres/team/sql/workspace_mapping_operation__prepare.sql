-- name: workspace_mapping_operation__prepare :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12
UPDATE interaction_gateway_workspace_mapping_operations
SET state = 'PENDING', selector_id = @arg4::uuid, provider_team_id = @arg5::text,
    provider_status = @arg6::text, provider_snapshot_sha256 = @arg7::text,
    provider_created_at = @arg8::timestamptz, provider_updated_at = @arg9::timestamptz,
    provider_observed_at = @arg10::timestamptz, effect_generation = @arg11::bigint,
    receipt_id = @arg12::uuid, failure_code = '', updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND state IN ('PENDING', 'AMBIGUOUS')
  AND fence = @arg2::bigint AND lease_token_sha256 = @arg3::text
  AND COALESCE(effect_generation, 0) < @arg11::bigint;
