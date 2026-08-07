-- name: mattermost_runtime_route__insert :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13,@arg14,@arg15,@arg16,@arg17,@arg18
INSERT INTO interaction_gateway_mattermost_runtime_routes(
    template_key, organization_id, project_id, mapping_owner_actor_id,
    mapping_id, mapping_version, mapping_generation, mapping_digest_sha256,
    provider_team_id, provider_snapshot_sha256, chat_id, role_id, locale,
    bot_stable_key, channel_id, session_id, owner_delivery, route_digest_sha256
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::uuid, @arg6::bigint,
    @arg7::bigint, @arg8::text, @arg9::text, @arg10::text, @arg11::uuid, @arg12::uuid,
    @arg13::text, @arg14::text, @arg15::text, NULLIF(@arg16::text, '')::uuid, @arg17::boolean, @arg18::text);
