UPDATE interaction_gateway_inbound_events
SET state = 'PROCESSING', attempts = attempts + 1,
    processing_expires_at = now() + $2::interval, updated_at = now()
WHERE id = $1 AND attempts < 32;
