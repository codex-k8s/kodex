SELECT session_id::text
FROM interaction_gateway_inbound_events
WHERE project_id = $1::uuid AND payload->>'channel_id' = $2
  AND session_id IS NOT NULL
  AND COALESCE(NULLIF(payload->>'root_post_id', ''), payload->>'post_id') = $3
ORDER BY updated_at DESC
LIMIT 1;
