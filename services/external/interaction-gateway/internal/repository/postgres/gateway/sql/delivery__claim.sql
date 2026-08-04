WITH candidate AS (
    SELECT id
    FROM interaction_gateway_deliveries
    WHERE ((state = 'PENDING' AND next_attempt_at <= now()) OR
           (state = 'DELIVERING' AND lease_expires_at < now()) OR
           state = 'PROVIDER_ACCEPTED')
      AND attempts < 32
    ORDER BY CASE WHEN state = 'PROVIDER_ACCEPTED' THEN 0 ELSE 1 END,
             next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE interaction_gateway_deliveries AS delivery
SET state = CASE WHEN delivery.state = 'PROVIDER_ACCEPTED' THEN 'PROVIDER_ACCEPTED' ELSE 'DELIVERING' END,
    attempts = delivery.attempts + 1,
    fence = delivery.fence + 1, lease_owner = $3, lease_token_sha256 = $1,
    lease_expires_at = now() + $2::interval, updated_at = now()
FROM candidate
WHERE delivery.id = candidate.id
RETURNING delivery.id;
