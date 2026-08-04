WITH candidate AS (
    SELECT id
    FROM interaction_gateway_deliveries
    WHERE ((state = 'PENDING' AND next_attempt_at <= clock_timestamp()) OR
           (state IN ('DELIVERING', 'PROVIDER_ACCEPTED') AND
            (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp()) AND
            next_attempt_at <= clock_timestamp()))
    ORDER BY CASE WHEN state = 'PROVIDER_ACCEPTED' THEN 0 ELSE 1 END,
             next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE interaction_gateway_deliveries AS delivery
SET state = CASE WHEN delivery.state = 'PROVIDER_ACCEPTED' THEN 'PROVIDER_ACCEPTED' ELSE 'DELIVERING' END,
    attempts = LEAST(delivery.attempts + CASE WHEN delivery.state = 'PROVIDER_ACCEPTED' THEN 0 ELSE 1 END, 32),
    ack_attempts = LEAST(delivery.ack_attempts + CASE WHEN delivery.state = 'PROVIDER_ACCEPTED' THEN 1 ELSE 0 END, 32),
    fence = delivery.fence + 1, lease_owner = $3, lease_token_sha256 = $1,
    lease_expires_at = clock_timestamp() + $2::interval, updated_at = clock_timestamp()
FROM candidate
WHERE delivery.id = candidate.id
RETURNING delivery.id;
