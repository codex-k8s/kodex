UPDATE integration_gateway.management_effects
   SET status = @status, dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL,
       payload = CASE WHEN @failure_category = '' THEN payload
                      ELSE jsonb_set(payload, '{failure_category}', to_jsonb(@failure_category::text)) END,
       updated_at = @updated_at
 WHERE effect_id = @effect_id AND status = 'CLAIMED'
   AND lease_id = @lease_id AND lease_fence = @lease_fence
   AND dispatch_state = 'DISPATCHED'
   AND (effect_kind <> 'PROVIDER_REVOKE'
        OR provider_phase IN ('SUCCEEDED', 'UNKNOWN', 'SKIPPED')
           AND secret_phase = 'SUCCEEDED' AND control_plane_phase = 'SUCCEEDED')
RETURNING effect_id
