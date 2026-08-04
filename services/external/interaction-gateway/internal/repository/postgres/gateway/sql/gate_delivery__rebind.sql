UPDATE interaction_gateway_deliveries
SET state = CASE WHEN provider_post_id = '' THEN 'PENDING' ELSE 'PROVIDER_ACCEPTED' END,
    owner_gate_version = $3, process_run_id = $4::uuid,
    process_version = NULLIF($5, 0), owner_gate_claim_token_ciphertext = $6,
    owner_gate_claim_fence = $7, owner_gate_claim_expires_at = $8,
    recipient_actor_id = $9::uuid, attempts = 0, fence = fence + 1,
    lease_owner = '', lease_token_sha256 = '', lease_expires_at = NULL, next_attempt_at = now(),
    last_error_code = '', updated_at = now()
WHERE id = $1::uuid AND owner_gate_id = $2::uuid
  AND owner_gate_payload_sha256 = $10 AND payload_sha256 = $11
  AND state IN ('PENDING', 'DELIVERING', 'PROVIDER_ACCEPTED', 'DEAD_LETTER');
