-- name: role_images_insert_build :one
INSERT INTO control_plane.image_builds
    (ref, organization_id, project_id, recipe_id, recipe_version, recipe_generation,
     specification, spec_sha256, immutable_build_sha256, attempt, maximum_attempts, stage)
SELECT $1, recipe.organization_id, recipe.project_id, recipe.id, recipe.version,
       recipe.generation, recipe.specification, recipe.spec_sha256, $3, 1, $4, 'QUEUED'
FROM control_plane.role_image_recipes recipe
WHERE recipe.organization_id = $2::uuid
  AND recipe.id = $5::uuid
  AND recipe.state = 'ACTIVE'
RETURNING id::text
