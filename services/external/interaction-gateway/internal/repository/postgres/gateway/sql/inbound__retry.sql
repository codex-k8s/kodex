UPDATE interaction_gateway_inbound_events
SET state = CASE WHEN $4::boolean THEN 'FAILED' ELSE 'PENDING' END,
    last_error_code = $2, next_attempt_at = $3, processing_expires_at = NULL,
    lease_owner = '', lease_token_sha256 = '',
    semantic_outcome = CASE WHEN $4::boolean THEN 'ERROR' ELSE '' END,
    response_message = CASE WHEN $4::boolean THEN $7 ELSE '' END,
    terminal_error_code = CASE WHEN $4::boolean THEN $2 ELSE '' END,
    next_action = CASE WHEN $4::boolean THEN $8 ELSE '' END,
    updated_at = clock_timestamp()
WHERE id = $1 AND state = 'PROCESSING' AND fence = $5 AND lease_token_sha256 = $6;
