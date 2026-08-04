INSERT INTO interaction_gateway_turn_watches (
    turn_id, inbound_id, organization_id, project_id
)
SELECT $2::uuid, inbound.id, inbound.organization_id, inbound.project_id
FROM interaction_gateway_inbound_events AS inbound
WHERE inbound.id = $1 AND inbound.fence = $3 AND inbound.lease_token_sha256 = $4
  AND inbound.state = 'PROCESSING'
ON CONFLICT (turn_id) DO UPDATE SET updated_at = interaction_gateway_turn_watches.updated_at
WHERE interaction_gateway_turn_watches.inbound_id = EXCLUDED.inbound_id
  AND interaction_gateway_turn_watches.organization_id = EXCLUDED.organization_id
  AND interaction_gateway_turn_watches.project_id = EXCLUDED.project_id;
