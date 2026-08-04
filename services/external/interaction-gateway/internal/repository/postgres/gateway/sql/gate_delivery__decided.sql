UPDATE interaction_gateway_deliveries
SET owner_gate_decided_at = COALESCE(owner_gate_decided_at, clock_timestamp()), updated_at = clock_timestamp()
WHERE id = $1 AND kind = 'OWNER_DECISION' AND state IN ('PROVIDER_ACCEPTED', 'DELIVERED')
  AND owner_gate_id IS NOT NULL;
