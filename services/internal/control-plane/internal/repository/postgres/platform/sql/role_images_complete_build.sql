-- name: platform__role_images_complete_build :one
UPDATE control_plane.image_builds
SET stage = 'COMPLETED',
    progress_percent = 100,
    staging_reference = $4,
    manifest_digest = $5,
    provenance_sha256 = $6,
    immutable_build_sha256 = $7,
    claimant_workload = NULL,
    authority_generation = 0,
    lease_token_sha256 = NULL,
    lease_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, stage, progress_percent, staging_reference, manifest_digest,
          provenance_sha256, immutable_build_sha256, updated_at
