UPDATE interaction_gateway_deliveries
SET state = 'PROVIDER_ACCEPTED', provider_post_id = $4,
    provider_receipt_sha256 = $5, root_post_id = $6,
    ack_attempts = ack_attempts + 1, updated_at = clock_timestamp()
WHERE id = $1 AND fence = $2 AND lease_token_sha256 = $3 AND state = 'DELIVERING';
