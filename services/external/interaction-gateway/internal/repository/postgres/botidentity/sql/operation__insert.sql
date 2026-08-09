-- name: operation__insert :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13,@arg14,@arg15,@arg16,@arg17,@arg18,@arg19,@arg20
INSERT INTO interaction_gateway_agent_bot_operations(
    operation_id, organization_id, project_id, actor_id, action, idempotency_key,
    agent_ref, expected_agent_version, predecessor_generation, identity_ref, selector_id,
    request_sha256, username, display_name, provider_correlation, state,
    fence, lease_owner, lease_token_sha256, lease_expires_at, recovery_deadline
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::text, @arg6::uuid,
          @arg7::uuid, @arg8::bigint, @arg9::bigint, NULLIF(@arg10::text, '')::uuid,
          NULLIF(@arg11::text, '')::uuid, @arg12::text, @arg13::text, @arg14::text,
          NULLIF(@arg15::text, '')::uuid, @arg16::text, 1, @arg17::text, @arg18::text,
          clock_timestamp() + @arg19::interval, clock_timestamp() + @arg20::interval)
ON CONFLICT DO NOTHING;
