INSERT INTO interaction_gateway_owner_gate_claim_requests (idempotency_key, state)
VALUES ($1, 'PENDING') ON CONFLICT DO NOTHING;
