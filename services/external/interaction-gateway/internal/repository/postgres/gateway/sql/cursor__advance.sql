INSERT INTO interaction_gateway_cursors (channel_id, last_post_at, organization_id, project_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id) DO UPDATE
SET last_post_at = GREATEST(interaction_gateway_cursors.last_post_at, EXCLUDED.last_post_at), updated_at = clock_timestamp()
WHERE interaction_gateway_cursors.organization_id = EXCLUDED.organization_id
  AND interaction_gateway_cursors.project_id = EXCLUDED.project_id;
