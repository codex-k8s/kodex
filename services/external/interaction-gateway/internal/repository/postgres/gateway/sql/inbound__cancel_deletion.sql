UPDATE interaction_gateway_inbound_events
SET state = 'COMPLETED', semantic_outcome = 'SUCCESS', response_message = $5,
    terminal_error_code = '', next_action = '', fence = fence + 1,
    lease_owner = '', lease_token_sha256 = '', processing_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid AND project_id = $2::uuid
  AND state IN ('PENDING', 'PROCESSING', 'WAITING_CLEANUP')
  AND kind IN ('CHANNEL_DELETE', 'THREAD_DELETE')
  AND ($3 = '' OR payload->>'chat_id' = $3)
  AND ($4 = '' OR session_id = $4::uuid);
