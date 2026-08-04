UPDATE interaction_gateway_owner_gate_claim_requests
SET state = 'PENDING', updated_at = now()
WHERE idempotency_key = $1 AND state IN ('PENDING', 'CLAIMED');
