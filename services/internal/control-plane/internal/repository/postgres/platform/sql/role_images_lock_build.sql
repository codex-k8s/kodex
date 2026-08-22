-- name: platform__role_images_lock_build :one
SELECT build.id::text, build.ref, recipe.ref, build.spec_sha256, build.stage,
       build.staging_reference, build.manifest_digest, build.provenance_sha256,
       build.immutable_build_sha256, build.safe_error_code, build.diagnostic_code,
       build.diagnostic_summary, COALESCE(build.lease_token_sha256, ''),
       COALESCE(build.claimant_workload, ''), build.version, build.recipe_version,
       build.recipe_generation, build.fence, build.authority_generation, build.attempt,
       build.progress_percent, build.lease_expires_at, build.created_at, build.updated_at,
       build.recipe_id::text, build.project_id::text, build.specification,
       recipe.policy_revision, recipe.policy_sha256,
       recipe.role_runtime_contract_revision, recipe.role_runtime_contract_sha256
FROM control_plane.image_builds build
JOIN control_plane.role_image_recipes recipe ON recipe.id = build.recipe_id
WHERE build.organization_id = $1::uuid
  AND build.ref = $2
FOR UPDATE OF build
