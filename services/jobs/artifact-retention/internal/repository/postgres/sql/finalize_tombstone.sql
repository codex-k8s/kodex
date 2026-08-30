-- name: finalize_tombstone :exec
UPDATE control_plane.artifacts
SET lifecycle_state = 'PURGED',
    file_name = 'purged',
    media_type = 'application/octet-stream',
    size_bytes = 0,
    digest = 'sha256:' || repeat('0', 64),
    scan_state = 'FAILED',
    preview_state = 'BLOCKED',
    purged_at = clock_timestamp(),
    retention_claim_owner = NULL,
    retention_claim_expires_at = NULL,
    version = version + 1
WHERE id = @artifact_id::uuid
  AND lifecycle_state = 'PURGE_PENDING'
  AND retention_claim_owner = @claim_owner
  AND retention_claim_generation = @claim_generation;
