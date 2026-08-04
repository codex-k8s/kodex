UPDATE interaction_gateway_inbound_events
SET state = 'PROCESSING', attempts = LEAST(attempts + 1, 32), fence = fence + 1,
    lease_owner = $3, lease_token_sha256 = $4,
    processing_expires_at = clock_timestamp() + $2::interval, updated_at = clock_timestamp()
WHERE id = $1
  AND ((state = 'PENDING' AND next_attempt_at <= clock_timestamp()) OR
       (state = 'PROCESSING' AND processing_expires_at <= clock_timestamp()));
