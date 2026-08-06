INSERT INTO interaction_gateway_team_catalog_cursors(
    cursor_id, organization_id, project_id, actor_id, catalog_offset, page_size, expires_at
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 1, 1, clock_timestamp() + interval '1 minute')
ON CONFLICT (cursor_id) DO UPDATE SET updated_at = clock_timestamp()
RETURNING organization_id::text, project_id::text, actor_id::text, catalog_offset;
