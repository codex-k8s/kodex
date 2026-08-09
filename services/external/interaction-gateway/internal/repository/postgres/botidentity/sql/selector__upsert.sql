-- name: selector__upsert :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7
INSERT INTO interaction_gateway_agent_bot_selectors(
    selector_id, organization_id, project_id, actor_id, identity_ref,
    provider_snapshot_sha256, expires_at
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::uuid, @arg6::text,
          clock_timestamp() + @arg7::interval)
ON CONFLICT (organization_id, project_id, actor_id, identity_ref) DO UPDATE SET
    provider_snapshot_sha256 = EXCLUDED.provider_snapshot_sha256,
    expires_at = EXCLUDED.expires_at, updated_at = clock_timestamp()
RETURNING selector_id::text;
