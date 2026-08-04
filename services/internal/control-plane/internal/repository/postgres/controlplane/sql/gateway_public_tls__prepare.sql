WITH prepared AS (
    INSERT INTO control_plane.gateway_public_tls_state (
        organization_id, project_id, workload_id,
        pending_generation, pending_certificate_sha256,
        pending_not_before, pending_not_after,
        pending_predecessor_generation,
        pending_predecessor_certificate_sha256, updated_at
    )
    SELECT @organization_id, @project_id, @workload_id,
        @generation, @certificate_sha256, @not_before, @not_after,
        @predecessor_generation, @predecessor_certificate_sha256, @updated_at
    WHERE @generation = 1
      AND @predecessor_generation = 0
      AND @predecessor_certificate_sha256 = ''
    ON CONFLICT (organization_id, project_id, workload_id) DO UPDATE
    SET pending_generation = EXCLUDED.pending_generation,
        pending_certificate_sha256 = EXCLUDED.pending_certificate_sha256,
        pending_not_before = EXCLUDED.pending_not_before,
        pending_not_after = EXCLUDED.pending_not_after,
        pending_predecessor_generation = EXCLUDED.pending_predecessor_generation,
        pending_predecessor_certificate_sha256 = EXCLUDED.pending_predecessor_certificate_sha256,
        updated_at = CASE
            WHEN gateway_public_tls_state.pending_generation IS NULL THEN EXCLUDED.updated_at
            ELSE gateway_public_tls_state.updated_at
        END
    WHERE (
            gateway_public_tls_state.pending_generation = EXCLUDED.pending_generation
            AND gateway_public_tls_state.pending_certificate_sha256 = EXCLUDED.pending_certificate_sha256
            AND gateway_public_tls_state.pending_not_before = EXCLUDED.pending_not_before
            AND gateway_public_tls_state.pending_not_after = EXCLUDED.pending_not_after
            AND gateway_public_tls_state.pending_predecessor_generation = @predecessor_generation
            AND gateway_public_tls_state.pending_predecessor_certificate_sha256 = @predecessor_certificate_sha256
        ) OR (
            gateway_public_tls_state.pending_generation IS NULL
            AND gateway_public_tls_state.applied_generation IS NOT NULL
            AND EXCLUDED.pending_generation = gateway_public_tls_state.applied_generation + 1
            AND @predecessor_generation = gateway_public_tls_state.applied_generation
            AND @predecessor_certificate_sha256 = gateway_public_tls_state.applied_certificate_sha256
        )
    RETURNING *
), authoritative AS (
    SELECT * FROM prepared
    UNION ALL
    SELECT current.*
    FROM control_plane.gateway_public_tls_state current
    WHERE current.organization_id = @organization_id
      AND current.project_id = @project_id
      AND current.workload_id = @workload_id
      AND (
        (current.applied_generation = @generation
            AND current.applied_certificate_sha256 = @certificate_sha256)
        OR (current.pending_generation = @generation
            AND current.pending_certificate_sha256 = @certificate_sha256
            AND current.pending_not_before = @not_before
            AND current.pending_not_after = @not_after
            AND current.pending_predecessor_generation = @predecessor_generation
            AND current.pending_predecessor_certificate_sha256 = @predecessor_certificate_sha256)
        OR (current.previous_generation = @generation
            AND current.previous_certificate_sha256 = @certificate_sha256
            AND current.overlap_expires_at > @updated_at)
      )
)
SELECT organization_id, project_id, workload_id,
    applied_generation, applied_certificate_sha256, applied_not_before, applied_not_after,
    pending_generation, pending_certificate_sha256, pending_not_before, pending_not_after,
    previous_generation, previous_certificate_sha256, previous_not_before, previous_not_after,
    overlap_expires_at, updated_at
FROM authoritative
LIMIT 1;
