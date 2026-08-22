-- name: platform__role_images_list_builds :many
SELECT build.ref, recipe.ref, build.spec_sha256, build.stage, build.staging_reference,
       build.manifest_digest, build.provenance_sha256, build.immutable_build_sha256,
       build.safe_error_code, build.diagnostic_code, build.diagnostic_summary,
       COALESCE(build.lease_token_sha256, ''), COALESCE(build.claimant_workload, ''),
       build.version, build.recipe_version, build.recipe_generation, build.fence,
       build.authority_generation, build.attempt, build.progress_percent,
       build.lease_expires_at, build.created_at, build.updated_at
FROM control_plane.image_builds build
JOIN control_plane.role_image_recipes recipe ON recipe.id = build.recipe_id
WHERE build.organization_id = $1::uuid
  AND build.recipe_id = $2::uuid
ORDER BY build.created_at DESC, build.ref
LIMIT 100
