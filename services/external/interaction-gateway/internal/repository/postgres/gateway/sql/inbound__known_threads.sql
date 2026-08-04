SELECT DISTINCT COALESCE(NULLIF(payload->>'root_post_id', ''), payload->>'post_id') AS root_post_id,
       payload->>'channel_id' AS channel_id
FROM interaction_gateway_inbound_events
WHERE project_id = $1::uuid
  AND session_id IS NOT NULL
  AND payload->>'channel_id' <> ''
  AND COALESCE(NULLIF(payload->>'root_post_id', ''), payload->>'post_id') <> ''
ORDER BY root_post_id
LIMIT $2;
