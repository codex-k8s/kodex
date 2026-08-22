-- name: platform__role_images_get_build_input :one
SELECT recipe.ref, build.recipe_version, build.recipe_generation, build.spec_sha256,
       build.specification, build.immutable_build_sha256,
       recipe.policy_revision, recipe.policy_sha256,
       recipe.role_runtime_contract_revision, recipe.role_runtime_contract_sha256
FROM control_plane.image_builds build
JOIN control_plane.role_image_recipes recipe ON recipe.id = build.recipe_id
WHERE build.organization_id = $1::uuid
  AND build.id = $2::uuid
  AND recipe.state = 'ACTIVE'
  AND recipe.version = build.recipe_version
  AND recipe.generation = build.recipe_generation
  AND recipe.spec_sha256 = build.spec_sha256
