WITH candidate AS (
    SELECT effect_id
      FROM integration_gateway.management_effects
     WHERE (status = 'PENDING' AND available_at <= clock_timestamp())
        OR (status = 'CLAIMED' AND lease_expires_at <= clock_timestamp())
     ORDER BY available_at, effect_id
     FOR UPDATE SKIP LOCKED
     LIMIT 1
), claimed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'CLAIMED', lease_id = @lease_id,
           lease_fence = effect.lease_fence + 1,
           lease_expires_at = clock_timestamp() + @lease_duration::interval,
           attempts = effect.attempts + 1, updated_at = clock_timestamp()
      FROM candidate
     WHERE effect.effect_id = candidate.effect_id
    RETURNING effect.*
), fenced_authorization AS (
    UPDATE integration_gateway.provider_authorization_attempts AS authorization
       SET lease_id = claimed.lease_id, lease_generation = claimed.lease_fence,
           lease_expires_at = claimed.lease_expires_at, updated_at = clock_timestamp()
      FROM claimed
     WHERE claimed.effect_kind = 'PROVIDER_AUTHORIZE'
       AND authorization.authorization_id = claimed.resource_id
       AND authorization.version = claimed.resource_version
       AND authorization.generation = claimed.resource_generation
       AND authorization.state IN ('PENDING', 'CODE_ISSUED')
    RETURNING authorization.authorization_id
)
SELECT effect_id, effect_kind, resource_kind, resource_id, resource_version,
       resource_generation, intent_sha256, status, lease_id, lease_fence,
       lease_expires_at, attempts, payload
  FROM claimed
 WHERE claimed.effect_kind <> 'PROVIDER_AUTHORIZE'
    OR EXISTS (SELECT 1 FROM fenced_authorization)
