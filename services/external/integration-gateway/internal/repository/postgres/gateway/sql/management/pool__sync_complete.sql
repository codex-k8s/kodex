WITH changed AS (
    UPDATE integration_gateway.managed_provider_pools
       SET status = CASE WHEN status = 'PENDING' THEN 'ACTIVE' ELSE status END,
           control_plane_resource_id = @control_plane_resource_id,
           control_plane_version = @control_plane_version,
           control_plane_sha256 = @control_plane_sha256,
           payload = @payload::jsonb, updated_at = @updated_at
     WHERE provider_pool_id = @provider_pool_id AND version = @expected_version
    RETURNING provider_pool_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
       AND effect.dispatch_state = 'DISPATCHED'
    RETURNING effect_id
)
SELECT provider_pool_id FROM changed WHERE EXISTS (SELECT 1 FROM completed)
