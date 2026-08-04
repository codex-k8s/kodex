SELECT provider_post_id, channel_id
FROM interaction_gateway_deliveries
WHERE kind = 'OWNER_DECISION' AND state = 'DELIVERED'
  AND owner_gate_decided_at IS NULL AND provider_post_id <> ''
  AND created_at >= clock_timestamp() - interval '30 days'
ORDER BY created_at DESC
LIMIT $1;
