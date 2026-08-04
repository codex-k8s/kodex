SELECT EXISTS (
    SELECT 1
    FROM control_plane.interaction_delivery_readback_grants
    WHERE id = @id
      AND organization_id = @organization_id
      AND project_id = @project_id
      AND delivery_id = @delivery_id
      AND credential_sha256 = @credential_sha256
      AND generation = @generation
      AND producer_id = 'control-plane.interaction-delivery-readback'
      AND purpose = 'INTERACTION_DELIVERY_READBACK_GRANT'
      AND workload_id = 'control-plane'
      AND caller_spiffe_id = 'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane'
      AND operation = 'interaction.delivery.read'
      AND permission = 'interaction.delivery.read'
      AND revoked_at IS NULL
      AND issued_at <= clock_timestamp()
      AND expires_at > clock_timestamp()
)
