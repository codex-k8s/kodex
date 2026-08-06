INSERT INTO interaction_gateway_team_catalog_cursors(
    cursor_id, organization_id, project_id, actor_id, catalog_offset, page_size, expires_at
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::integer, $6::integer,
          clock_timestamp() + $7::interval)
ON CONFLICT (cursor_id) DO UPDATE SET
    expires_at = EXCLUDED.expires_at,
    updated_at = clock_timestamp()
WHERE interaction_gateway_team_catalog_cursors.organization_id = EXCLUDED.organization_id
  AND interaction_gateway_team_catalog_cursors.project_id = EXCLUDED.project_id
  AND interaction_gateway_team_catalog_cursors.actor_id = EXCLUDED.actor_id
  AND interaction_gateway_team_catalog_cursors.catalog_offset = EXCLUDED.catalog_offset
  AND interaction_gateway_team_catalog_cursors.page_size = EXCLUDED.page_size
RETURNING cursor_id::text;
