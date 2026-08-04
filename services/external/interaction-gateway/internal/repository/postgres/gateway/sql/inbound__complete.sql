UPDATE interaction_gateway_inbound_events
SET state = 'COMPLETED', session_id = NULLIF($2, '')::uuid, turn_id = NULLIF($3, '')::uuid,
    processing_expires_at = NULL, lease_owner = '', lease_token_sha256 = '',
    last_error_code = '', semantic_outcome = 'SUCCESS', response_message = $6,
    terminal_error_code = '', next_action = '', updated_at = clock_timestamp()
WHERE id = $1 AND state = 'PROCESSING' AND fence = $4 AND lease_token_sha256 = $5;
