SELECT organization_id, project_id, workload_id,
    applied_generation, applied_certificate_sha256, applied_not_before, applied_not_after,
    pending_generation, pending_certificate_sha256, pending_not_before, pending_not_after,
    previous_generation, previous_certificate_sha256, previous_not_before, previous_not_after,
    overlap_expires_at, updated_at
FROM control_plane.gateway_public_tls_state
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND workload_id = @workload_id
  AND (
    (applied_generation = @generation AND applied_certificate_sha256 = @certificate_sha256)
    OR (pending_generation = @generation AND pending_certificate_sha256 = @certificate_sha256)
    OR (previous_generation = @generation AND previous_certificate_sha256 = @certificate_sha256
        AND overlap_expires_at > @checked_at)
  );
