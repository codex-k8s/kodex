-- name: platform__role_images_claim_build :one
UPDATE control_plane.image_builds
SET attempt = $4,
    stage = 'MATERIALIZATION',
    progress_percent = 0,
    claimant_workload = $5,
    authority_generation = $6,
    fence = $7,
    lease_token_sha256 = $8,
    lease_expires_at = $9,
    safe_error_code = '',
    diagnostic_code = '',
    diagnostic_summary = '',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING ref, (SELECT recipe.ref FROM control_plane.role_image_recipes recipe WHERE recipe.id = recipe_id), spec_sha256, stage, staging_reference, manifest_digest,
          provenance_sha256, immutable_build_sha256, safe_error_code, diagnostic_code,
          diagnostic_summary, COALESCE(lease_token_sha256, ''), COALESCE(claimant_workload, ''),
          version, recipe_version, recipe_generation, fence, authority_generation,
          attempt, progress_percent, lease_expires_at, created_at, updated_at
