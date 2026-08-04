UPDATE interaction_gateway_inbound_events
SET payload = $2, session_id = NULLIF($3, '')::uuid,
    prompt_artifact_id = NULLIF($4, '')::uuid, attachment_artifacts = $5,
    state = $6,
    processing_expires_at = CASE WHEN $6 = 'PROCESSING' THEN processing_expires_at ELSE NULL END,
    next_attempt_at = $7, updated_at = now()
WHERE id = $1 AND state IN ('PROCESSING', 'WAITING_SCAN');
