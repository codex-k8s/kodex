UPDATE interaction_gateway_deliveries
SET owner_gate_decided_at = COALESCE(owner_gate_decided_at, now()), updated_at = now()
WHERE id = $1 AND kind = 'OWNER_DECISION' AND state IN ('PROVIDER_ACCEPTED', 'DELIVERED')
  AND owner_gate_id IS NOT NULL;
