-- name: role_images_admission_current :one
SELECT EXISTS (
 SELECT 1 FROM control_plane.image_artifacts artifact
 JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
 JOIN control_plane.image_builds build ON build.id = artifact.build_id
 WHERE artifact.organization_id = @organization_id::uuid AND artifact.id = @artifact_id::uuid
   AND recipe.organization_id = artifact.organization_id AND recipe.project_id IS NOT DISTINCT FROM artifact.project_id
   AND recipe.state = 'ACTIVE' AND recipe.version = artifact.recipe_version
   AND recipe.generation = artifact.recipe_generation AND recipe.spec_sha256 = artifact.spec_sha256
   AND recipe.policy_revision = artifact.policy_revision AND recipe.policy_sha256 = artifact.policy_sha256
   AND recipe.role_runtime_contract_revision = artifact.role_runtime_contract_revision
   AND recipe.role_runtime_contract_sha256 = artifact.role_runtime_contract_sha256
   AND build.organization_id = artifact.organization_id AND build.recipe_id = artifact.recipe_id
   AND build.stage = 'COMPLETED' AND build.attempt = artifact.build_attempt
   AND build.id = (SELECT latest.id FROM control_plane.image_builds latest
      WHERE latest.organization_id = artifact.organization_id AND latest.recipe_id = artifact.recipe_id
      ORDER BY latest.created_at DESC, latest.updated_at DESC, latest.attempt DESC, latest.ref DESC LIMIT 1)
)
