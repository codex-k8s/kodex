UPDATE interaction_gateway_deliveries
SET state = 'DELIVERED', delivery_recorded_at = CASE WHEN owner_gate_id IS NULL THEN delivery_recorded_at ELSE clock_timestamp() END,
    owner_gate_claim_token_ciphertext = CASE WHEN owner_gate_id IS NULL THEN owner_gate_claim_token_ciphertext ELSE NULL END,
    lease_owner = '', lease_expires_at = NULL, lease_token_sha256 = '', last_error_code = '', updated_at = clock_timestamp()
WHERE id = $1 AND fence = $2 AND lease_token_sha256 = $3 AND state = 'PROVIDER_ACCEPTED';
