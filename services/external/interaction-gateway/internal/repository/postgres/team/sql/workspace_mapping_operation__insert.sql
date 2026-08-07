-- name: workspace_mapping_operation__insert :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13,@arg14,@arg15,@arg16,@arg17,@arg18,@arg19,@arg20,@arg21,@arg22,@arg23
INSERT INTO interaction_gateway_workspace_mapping_operations(
    operation_id, organization_id, project_id, actor_id, action, idempotency_key,
    request_sha256, mapping_id, expected_mapping_version, expected_mapping_generation,
    display_name, selector_id, provider_team_id, provider_status,
    provider_snapshot_sha256, provider_created_at, provider_updated_at, provider_observed_at,
    state, fence, lease_owner, lease_token_sha256, lease_expires_at, recovery_deadline, create_operation_id)
VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::text, @arg6::uuid,
    @arg7::text, NULLIF(@arg8::text, '')::uuid, @arg9::bigint, @arg10::bigint,
    @arg11::text, @arg12::uuid, @arg13::text, @arg14::text,
    @arg15::text, @arg16::timestamptz, @arg17::timestamptz, @arg18::timestamptz,
    'PENDING', 1, @arg19::text, @arg20::text,
    clock_timestamp() + @arg21::interval, clock_timestamp() + @arg22::interval,
    NULLIF(@arg23::text, '')::uuid)
ON CONFLICT (organization_id, project_id, actor_id, action, idempotency_key) DO NOTHING;
