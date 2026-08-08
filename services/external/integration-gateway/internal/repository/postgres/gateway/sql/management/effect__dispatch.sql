UPDATE integration_gateway.management_effects AS effect
   SET dispatch_state = 'DISPATCHED', updated_at = clock_timestamp()
 WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
   AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
   AND effect.lease_expires_at > clock_timestamp() AND effect.dispatch_state = 'PENDING'
   AND effect.input_sha256 = effect.intent_sha256
   AND CASE effect.owner_kind
         WHEN 'managed_provider_connection' THEN EXISTS (
           SELECT 1 FROM integration_gateway.managed_provider_connections AS owner
            WHERE owner.connection_id = effect.owner_id AND owner.version = effect.owner_version
              AND owner.generation = effect.owner_generation AND owner.status = effect.owner_status
            FOR UPDATE)
         WHEN 'managed_provider_pool' THEN EXISTS (
           SELECT 1 FROM integration_gateway.managed_provider_pools AS owner
            WHERE owner.provider_pool_id = effect.owner_id AND owner.version = effect.owner_version
              AND owner.status = effect.owner_status FOR UPDATE)
         WHEN 'git_source_binding' THEN EXISTS (
           SELECT 1 FROM integration_gateway.git_source_bindings AS owner
            WHERE owner.binding_id = effect.owner_id AND owner.version = effect.owner_version
              AND owner.generation = effect.owner_generation AND owner.status = effect.owner_status
            FOR UPDATE)
         ELSE false
       END
RETURNING effect.effect_id
