-- name: mattermost_runtime_route__admission :one
-- params: @arg1,@arg2,@arg3,@arg4
SELECT template_key::text, organization_id::text, project_id::text, mapping_owner_actor_id::text,
       mapping_id::text, mapping_version, mapping_generation, mapping_digest_sha256,
       provider_team_id, provider_snapshot_sha256, chat_id::text, role_id::text, locale,
       bot_stable_key, channel_id, COALESCE(session_id::text, ''), owner_delivery,
       route_digest_sha256, created_at, updated_at
FROM interaction_gateway_mattermost_runtime_routes
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid
  AND mapping_owner_actor_id = @arg3::uuid AND provider_team_id = @arg4::text
ORDER BY template_key LIMIT 1;
