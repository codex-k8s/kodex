-- name: runtime_configuration__activate_system_image :one
UPDATE control_plane.role_image_recipes recipe
SET active_image_artifact_id = artifact.id,
    version = CASE
        WHEN recipe.active_image_artifact_id IS DISTINCT FROM artifact.id THEN recipe.version + 1
        ELSE recipe.version
    END,
    updated_at = CASE
        WHEN recipe.active_image_artifact_id IS DISTINCT FROM artifact.id THEN clock_timestamp()
        ELSE recipe.updated_at
    END
FROM control_plane.image_artifacts artifact
WHERE recipe.organization_id = @organization_id::uuid
  AND recipe.project_id = @project_id::uuid
  AND recipe.ref = @recipe_ref
  AND recipe.state = 'ACTIVE'
  AND artifact.organization_id = recipe.organization_id
  AND artifact.project_id = recipe.project_id
  AND artifact.recipe_id = recipe.id
  AND artifact.id = @artifact_id::uuid
  AND artifact.ref = @artifact_ref
  AND artifact.recipe_generation = @recipe_generation
  AND artifact.spec_sha256 = recipe.spec_sha256
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.promotion_state = 'PROMOTED'
  AND artifact.promoted_reference = @image_reference
  AND artifact.manifest_digest = @manifest_digest
RETURNING recipe.id::text
