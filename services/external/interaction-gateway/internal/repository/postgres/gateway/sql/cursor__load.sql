SELECT channel_id, last_post_at FROM interaction_gateway_cursors WHERE channel_id = ANY($1::text[]);
