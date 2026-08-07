-- name: team_operation__insert :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13
INSERT INTO interaction_gateway_team_operations(
    operation_id, organization_id, project_id, actor_id, kind,
    idempotency_key, request_sha256, provider_correlation, display_name, slug, state,
    fence, lease_owner, lease_token_sha256, lease_expires_at, recovery_deadline, retry_not_before
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, 'CREATE',
          @arg5::uuid, @arg6::text, @arg7::uuid, @arg8::text, @arg9::text, 'PENDING',
          1, @arg10::text, @arg11::text, clock_timestamp() + @arg12::interval,
          clock_timestamp() + @arg13::interval, clock_timestamp())
ON CONFLICT (organization_id, project_id, actor_id, kind, idempotency_key) DO NOTHING;
