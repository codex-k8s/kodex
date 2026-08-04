UPDATE interaction_gateway_inbound_events
SET state = 'COMPLETED', session_id = $2::uuid, turn_id = $3::uuid,
    processing_expires_at = NULL, last_error_code = '', updated_at = now()
WHERE id = $1 AND state = 'PROCESSING';
