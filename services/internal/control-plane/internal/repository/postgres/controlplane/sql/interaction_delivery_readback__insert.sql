WITH revoked AS (
    UPDATE control_plane.interaction_delivery_readback_grants
    SET revoked_at = clock_timestamp()
    WHERE organization_id = @organization_id
      AND project_id = @project_id
      AND delivery_id = @delivery_id
      AND id <> @id
      AND revoked_at IS NULL
    RETURNING id
)
INSERT INTO control_plane.interaction_delivery_readback_grants (
    id, organization_id, project_id, actor_id, delivery_id, producer_id, purpose,
    workload_id, caller_spiffe_id, operation, permission, credential_sha256,
    generation, keyset_revision, keyset_high_watermark, keyset_sha256,
    issued_at, expires_at, readiness
) VALUES (
    @id, @organization_id, @project_id, @actor_id, @delivery_id, @producer_id, @purpose,
    @workload_id, @caller_spiffe_id, @operation, @permission, @credential_sha256,
    @generation, @keyset_revision, @keyset_high_watermark, @keyset_sha256,
    @issued_at, @expires_at, @readiness
)
ON CONFLICT (id) DO UPDATE SET credential_sha256 = EXCLUDED.credential_sha256
WHERE control_plane.interaction_delivery_readback_grants.credential_sha256 = EXCLUDED.credential_sha256
