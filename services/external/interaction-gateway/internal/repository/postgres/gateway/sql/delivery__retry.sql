UPDATE interaction_gateway_deliveries
SET state = CASE
        WHEN $5::boolean THEN 'DEAD_LETTER'
        WHEN state = 'PROVIDER_ACCEPTED' THEN 'PROVIDER_ACCEPTED'
        ELSE 'PENDING'
    END,
    last_error_code = $3, next_attempt_at = $4,
    lease_owner = '', lease_expires_at = NULL, lease_token_sha256 = '', updated_at = now()
WHERE id = $1 AND fence = $2 AND state IN ('DELIVERING', 'PROVIDER_ACCEPTED');
