WITH prepared AS (
    INSERT INTO control_plane.gateway_public_tls_state (
        organization_id, project_id, workload_id,
        pending_generation, pending_certificate_sha256,
        pending_not_before, pending_not_after,
        pending_predecessor_generation,
        pending_predecessor_certificate_sha256, updated_at
    )
    SELECT @organization_id::uuid, @project_id::uuid, @workload_id::text,
        @generation::bigint, @certificate_sha256::text, @not_before::timestamptz, @not_after::timestamptz,
        @predecessor_generation::bigint, @predecessor_certificate_sha256::text, @updated_at::timestamptz
    WHERE @generation::bigint = 1
      AND @predecessor_generation::bigint = 0
      AND @predecessor_certificate_sha256::text = ''
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
            AND gateway_public_tls_state.pending_predecessor_generation = @predecessor_generation::bigint
            AND gateway_public_tls_state.pending_predecessor_certificate_sha256 = @predecessor_certificate_sha256::text
        ) OR (
            gateway_public_tls_state.pending_generation IS NULL
            AND gateway_public_tls_state.applied_generation IS NOT NULL
            AND EXCLUDED.pending_generation = gateway_public_tls_state.applied_generation + 1
            AND @predecessor_generation::bigint = gateway_public_tls_state.applied_generation
            AND @predecessor_certificate_sha256::text = gateway_public_tls_state.applied_certificate_sha256
        )
    RETURNING *
), authoritative AS (
    SELECT * FROM prepared
    UNION ALL
    SELECT current.*
    FROM control_plane.gateway_public_tls_state current
    WHERE current.organization_id = @organization_id::uuid
      AND current.project_id = @project_id::uuid
      AND current.workload_id = @workload_id::text
      AND (
        (current.applied_generation = @generation::bigint
            AND current.applied_certificate_sha256 = @certificate_sha256::text)
        OR (current.pending_generation = @generation::bigint
            AND current.pending_certificate_sha256 = @certificate_sha256::text
            AND current.pending_not_before = @not_before::timestamptz
            AND current.pending_not_after = @not_after::timestamptz
            AND current.pending_predecessor_generation = @predecessor_generation::bigint
            AND current.pending_predecessor_certificate_sha256 = @predecessor_certificate_sha256::text)
        OR (current.previous_generation = @generation::bigint
            AND current.previous_certificate_sha256 = @certificate_sha256::text
            AND current.overlap_expires_at > @updated_at::timestamptz)
      )
)
SELECT organization_id, project_id, workload_id,
    applied_generation, applied_certificate_sha256, applied_not_before, applied_not_after,
    pending_generation, pending_certificate_sha256, pending_not_before, pending_not_after,
    previous_generation, previous_certificate_sha256, previous_not_before, previous_not_after,
    overlap_expires_at, updated_at
FROM authoritative
LIMIT 1;
