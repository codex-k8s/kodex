UPDATE interaction_gateway_inbound_events
SET payload = $2, session_id = NULLIF($3, '')::uuid,
    prompt_artifact_id = NULLIF($4, '')::uuid, attachment_artifacts = $5,
    state = $6,
    processing_expires_at = CASE WHEN $6 = 'PROCESSING' THEN processing_expires_at ELSE NULL END,
    lease_owner = CASE WHEN $6 = 'PROCESSING' THEN lease_owner ELSE '' END,
    lease_token_sha256 = CASE WHEN $6 = 'PROCESSING' THEN lease_token_sha256 ELSE '' END,
    next_attempt_at = $7, semantic_outcome = $10, response_message = $11,
    next_action = $12, updated_at = clock_timestamp()
WHERE id = $1 AND state = 'PROCESSING' AND fence = $8 AND lease_token_sha256 = $9;
