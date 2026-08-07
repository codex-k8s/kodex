WITH renewed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET lease_expires_at = clock_timestamp() + @lease_duration::interval,
           updated_at = clock_timestamp()
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
       AND effect.lease_expires_at > clock_timestamp()
    RETURNING effect.effect_id, effect.effect_kind, effect.resource_id,
              effect.resource_version, effect.resource_generation,
              effect.lease_expires_at
), authorization_renewed AS (
    UPDATE integration_gateway.provider_authorization_attempts AS authorization
       SET lease_expires_at = renewed.lease_expires_at,
           updated_at = clock_timestamp()
      FROM renewed
     WHERE renewed.effect_kind = 'PROVIDER_AUTHORIZE'
       AND authorization.authorization_id = renewed.resource_id
       AND authorization.version = renewed.resource_version
       AND authorization.generation = renewed.resource_generation
       AND authorization.lease_id = @lease_id
       AND authorization.lease_generation = @lease_fence
       AND authorization.state IN ('PENDING', 'CODE_ISSUED')
    RETURNING authorization.authorization_id
)
SELECT effect_id
  FROM renewed
 WHERE renewed.effect_kind <> 'PROVIDER_AUTHORIZE'
    OR EXISTS (SELECT 1 FROM authorization_renewed)
