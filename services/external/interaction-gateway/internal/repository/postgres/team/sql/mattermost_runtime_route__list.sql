-- name: mattermost_runtime_route__list :many
-- params:
SELECT template_key::text, organization_id::text, project_id::text, mapping_owner_actor_id::text,
       mapping_id::text, mapping_version, mapping_generation, mapping_digest_sha256,
       provider_team_id, provider_snapshot_sha256, chat_id::text, role_id::text, locale,
       bot_stable_key, channel_id, COALESCE(session_id::text, ''), owner_delivery,
       route_digest_sha256, created_at, updated_at
FROM interaction_gateway_mattermost_runtime_routes
ORDER BY project_id, chat_id, template_key;
