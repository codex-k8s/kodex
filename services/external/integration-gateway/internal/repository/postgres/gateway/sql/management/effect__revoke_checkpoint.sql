UPDATE integration_gateway.management_effects AS effect
   SET provider_phase = CASE @step
         WHEN 'PROVIDER_DISPATCHED' THEN 'DISPATCHED'
         WHEN 'PROVIDER_SUCCEEDED' THEN 'SUCCEEDED'
         WHEN 'PROVIDER_UNKNOWN' THEN 'UNKNOWN'
         ELSE provider_phase
       END,
       secret_phase = CASE WHEN @step = 'SECRET_SUCCEEDED' THEN 'SUCCEEDED' ELSE secret_phase END,
       control_plane_phase = CASE WHEN @step = 'CONTROL_PLANE_SUCCEEDED' THEN 'SUCCEEDED' ELSE control_plane_phase END,
       checkpoint = checkpoint || jsonb_build_object(@step, @updated_at::text),
       updated_at = @updated_at
 WHERE effect.effect_id = @effect_id AND effect.effect_kind = 'PROVIDER_REVOKE'
   AND effect.status = 'CLAIMED' AND effect.lease_id = @lease_id
   AND effect.lease_fence = @lease_fence AND effect.dispatch_state = 'DISPATCHED'
   AND CASE @step
         WHEN 'PROVIDER_DISPATCHED' THEN provider_phase = 'PENDING'
         WHEN 'PROVIDER_SUCCEEDED' THEN provider_phase = 'DISPATCHED'
         WHEN 'PROVIDER_UNKNOWN' THEN provider_phase = 'DISPATCHED'
         WHEN 'SECRET_SUCCEEDED' THEN provider_phase IN ('SUCCEEDED', 'UNKNOWN', 'SKIPPED') AND secret_phase = 'PENDING'
         WHEN 'CONTROL_PLANE_SUCCEEDED' THEN secret_phase = 'SUCCEEDED' AND control_plane_phase = 'PENDING'
         ELSE false
       END
RETURNING provider_phase, secret_phase, control_plane_phase
