INSERT INTO control_plane.gateway_public_tls_state (
    organization_id, project_id, workload_id, generation, certificate_sha256,
    not_before, not_after, updated_at
) SELECT
    @organization_id, @project_id, @workload_id, @generation, @certificate_sha256,
    @not_before, @not_after, @updated_at
WHERE (
    @generation = 1
    AND @predecessor_generation = 0
    AND @predecessor_certificate_sha256 = ''
) OR EXISTS (
    SELECT 1
    FROM control_plane.gateway_public_tls_state current
    WHERE current.organization_id = @organization_id
      AND current.project_id = @project_id
      AND current.workload_id = @workload_id
)
ON CONFLICT (organization_id, project_id, workload_id) DO UPDATE
SET generation = EXCLUDED.generation,
    certificate_sha256 = EXCLUDED.certificate_sha256,
    not_before = EXCLUDED.not_before,
    not_after = EXCLUDED.not_after,
    updated_at = EXCLUDED.updated_at
WHERE (
        EXCLUDED.generation = gateway_public_tls_state.generation
        AND EXCLUDED.certificate_sha256 = gateway_public_tls_state.certificate_sha256
        AND EXCLUDED.not_before = gateway_public_tls_state.not_before
        AND EXCLUDED.not_after = gateway_public_tls_state.not_after
    ) OR (
        EXCLUDED.generation = gateway_public_tls_state.generation + 1
        AND @predecessor_generation = gateway_public_tls_state.generation
        AND @predecessor_certificate_sha256 = gateway_public_tls_state.certificate_sha256
    )
RETURNING organization_id, project_id, workload_id, generation,
    certificate_sha256, not_before, not_after, updated_at;
