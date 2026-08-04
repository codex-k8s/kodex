INSERT INTO interaction_gateway_cursors (channel_id, last_post_at)
VALUES ($1, $2)
ON CONFLICT (channel_id) DO UPDATE
SET last_post_at = GREATEST(interaction_gateway_cursors.last_post_at, EXCLUDED.last_post_at), updated_at = now();
