SELECT EXISTS (
    SELECT 1 FROM interaction_gateway_deliveries
    WHERE owner_gate_id IS NOT NULL AND state <> 'DELIVERED'
      AND owner_gate_claim_expires_at > now()
);
