INSERT INTO interaction_gateway_cursors (
    channel_id, last_post_at, organization_id, project_id, updated_at
) VALUES ($1, $2, $3::uuid, $4::uuid, clock_timestamp())
ON CONFLICT (channel_id) DO UPDATE SET
    last_post_at = EXCLUDED.last_post_at,
    organization_id = EXCLUDED.organization_id,
    project_id = EXCLUDED.project_id,
    updated_at = clock_timestamp()
RETURNING organization_id, project_id, last_post_at;
