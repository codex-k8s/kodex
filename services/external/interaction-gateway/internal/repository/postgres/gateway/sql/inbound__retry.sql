UPDATE interaction_gateway_inbound_events
SET state = CASE WHEN $4::boolean THEN 'FAILED' ELSE 'PENDING' END,
    last_error_code = $2, next_attempt_at = $3, processing_expires_at = NULL, updated_at = now()
WHERE id = $1 AND state = 'PROCESSING';
