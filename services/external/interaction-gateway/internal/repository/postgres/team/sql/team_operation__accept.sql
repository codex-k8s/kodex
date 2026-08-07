-- name: team_operation__accept :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13
UPDATE interaction_gateway_team_operations
SET state = 'PROVIDER_ACCEPTED',
    selector_id = @arg2::uuid,
    provider_team_id = @arg3::text,
    provider_status = @arg4::text,
    provider_snapshot_sha256 = @arg5::text,
    provider_causality_sha256 = @arg6::text,
    provider_receipt_sha256 = @arg7::text,
    provider_generation = @arg8::bigint,
    provider_created_at = @arg9::timestamptz,
    provider_updated_at = @arg10::timestamptz,
    provider_observed_at = @arg11::timestamptz,
    failure_code = '',
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid
  AND state IN ('EFFECT_PENDING', 'AMBIGUOUS')
  AND fence = @arg12::bigint
  AND lease_token_sha256 = @arg13::text;
