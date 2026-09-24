-- name: role_images_cancel_exact_build :one
WITH cancelled AS (
  UPDATE control_plane.image_builds build
  SET stage = 'CANCELLED',
      safe_error_code = 'IMAGE_BUILD_CANCELLED_BY_USER',
      diagnostic_code = 'BUILD_CANCELLED_BY_USER',
      diagnostic_summary = 'Build was cancelled by the owner',
      claimant_workload = NULL,
      authority_generation = authority_generation + 1,
      fence = fence + 1,
      lease_token_sha256 = NULL,
      lease_expires_at = NULL,
      version = version + 1,
      updated_at = clock_timestamp()
  WHERE build.organization_id = $1::uuid
    AND build.recipe_id = $2::uuid
    AND build.ref = $3
    AND build.stage NOT IN ('COMPLETED', 'CANCELLED', 'DEAD_LETTER')
  RETURNING build.*
)
SELECT build.ref, recipe.ref, build.spec_sha256, build.stage, build.staging_reference,
       build.manifest_digest, build.provenance_sha256, build.immutable_build_sha256,
       build.safe_error_code, build.diagnostic_code, build.diagnostic_summary,
       COALESCE(build.lease_token_sha256, ''), COALESCE(build.claimant_workload, ''),
       build.version, build.recipe_version, build.recipe_generation, build.fence,
       build.authority_generation, build.attempt, build.progress_percent,
       build.lease_expires_at, build.created_at, build.updated_at,
       build.specification
FROM cancelled build
JOIN control_plane.role_image_recipes recipe ON recipe.id = build.recipe_id
