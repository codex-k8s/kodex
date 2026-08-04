SELECT idempotency_key FROM interaction_gateway_owner_gate_claim_requests
WHERE state = 'PENDING' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED;
