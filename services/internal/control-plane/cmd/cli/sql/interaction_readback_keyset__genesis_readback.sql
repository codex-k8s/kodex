SELECT fence.keyset_revision,
       fence.high_watermark,
       fence.served_generation,
       fence.keyset_sha256,
       jsonb_agg(jsonb_build_object(
           'generation', history.generation,
           'status', history.status,
           'kid', history.kid,
           'thumbprint_sha256', history.thumbprint_sha256
       ) ORDER BY history.generation)
  FROM control_plane.interaction_delivery_readback_keyset_fence AS fence
 CROSS JOIN control_plane.interaction_delivery_readback_key_history AS history
 WHERE EXISTS (
       SELECT 1 FROM control_plane.interaction_delivery_readback_keyset_audit WHERE action = 'GENESIS'
 )
 GROUP BY fence.keyset_revision, fence.high_watermark, fence.served_generation, fence.keyset_sha256;
