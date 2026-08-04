INSERT INTO interaction_gateway_download_grants (
    id, generation, organization_id, project_id, actor_id, mattermost_user_id,
    team_id, channel_id, session_id, turn_id, artifact, issued_payload_sha256, expires_at
) VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8, $9::uuid, $10::uuid, $11::jsonb, $12, $13)
ON CONFLICT (id) DO NOTHING;
