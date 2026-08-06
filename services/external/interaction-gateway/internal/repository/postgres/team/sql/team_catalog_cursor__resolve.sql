SELECT catalog_offset
FROM interaction_gateway_team_catalog_cursors
WHERE cursor_id = $1::uuid
  AND organization_id = $2::uuid
  AND project_id = $3::uuid
  AND actor_id = $4::uuid
  AND page_size = $5::integer
  AND expires_at > clock_timestamp();
