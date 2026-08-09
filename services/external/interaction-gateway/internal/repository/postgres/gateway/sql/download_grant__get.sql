SELECT id::text, generation, organization_id::text, project_id::text, actor_id::text,
       mattermost_user_id, team_id, channel_id, bot_stable_key, bot_provider_user_id,
       bot_provider_generation, session_id::text, turn_id::text,
       artifact, expires_at, consumed_at, revoked_at, issued_payload_sha256,
       authenticated_user_id, authenticated_at
FROM interaction_gateway_download_grants
WHERE id = $1::uuid;
