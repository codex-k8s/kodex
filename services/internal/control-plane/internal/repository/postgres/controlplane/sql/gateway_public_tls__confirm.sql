WITH confirmed AS (
    UPDATE control_plane.gateway_public_tls_state
    SET previous_generation = applied_generation,
        previous_certificate_sha256 = applied_certificate_sha256,
        previous_not_before = applied_not_before,
        previous_not_after = applied_not_after,
        overlap_expires_at = CASE WHEN applied_generation IS NULL THEN NULL ELSE @overlap_expires_at END,
        applied_generation = pending_generation,
        applied_certificate_sha256 = pending_certificate_sha256,
        applied_not_before = pending_not_before,
        applied_not_after = pending_not_after,
        pending_generation = NULL,
        pending_certificate_sha256 = NULL,
        pending_not_before = NULL,
        pending_not_after = NULL,
        pending_predecessor_generation = NULL,
        pending_predecessor_certificate_sha256 = NULL,
        updated_at = @updated_at
    WHERE organization_id = @organization_id
      AND project_id = @project_id
      AND workload_id = @workload_id
      AND pending_generation = @generation
      AND pending_certificate_sha256 = @certificate_sha256
    RETURNING *
), authoritative AS (
    SELECT * FROM confirmed
    UNION ALL
    SELECT current.*
    FROM control_plane.gateway_public_tls_state current
    WHERE current.organization_id = @organization_id
      AND current.project_id = @project_id
      AND current.workload_id = @workload_id
      AND current.applied_generation = @generation
      AND current.applied_certificate_sha256 = @certificate_sha256
)
SELECT organization_id, project_id, workload_id,
    applied_generation, applied_certificate_sha256, applied_not_before, applied_not_after,
    pending_generation, pending_certificate_sha256, pending_not_before, pending_not_after,
    previous_generation, previous_certificate_sha256, previous_not_before, previous_not_after,
    overlap_expires_at, updated_at
FROM authoritative
LIMIT 1;
