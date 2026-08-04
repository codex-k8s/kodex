UPDATE interaction_gateway_owner_gate_claim_requests
SET state = 'CLAIMED', owner_gate_id = $2::uuid, delivery_id = $3::uuid, updated_at = now()
WHERE idempotency_key = $1 AND state = 'PENDING';
