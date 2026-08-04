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
  FROM control_plane.mattermost_event_verifier_fence AS fence
  JOIN control_plane.mattermost_event_key_history AS history
    ON history.producer_id = fence.producer_id
  JOIN control_plane.mattermost_event_keyset_genesis_audit AS audit
    ON audit.producer_id = fence.producer_id
 WHERE fence.producer_id = $1
 GROUP BY fence.keyset_revision, fence.high_watermark, fence.served_generation, fence.keyset_sha256;
