UPDATE interaction_gateway_deliveries
SET state = 'PROVIDER_ACCEPTED', provider_post_id = $4,
    provider_receipt_sha256 = $5, root_post_id = $6,
    lease_expires_at = NULL, lease_token_sha256 = '', updated_at = now()
WHERE id = $1 AND fence = $2 AND lease_token_sha256 = $3 AND state = 'DELIVERING';
