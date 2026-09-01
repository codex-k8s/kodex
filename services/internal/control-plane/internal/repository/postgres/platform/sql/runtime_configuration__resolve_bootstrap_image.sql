-- name: runtime_configuration__resolve_bootstrap_image :one
SELECT artifact.id::text,
       artifact.ref,
       recipe.ref,
       artifact.recipe_generation,
       artifact.promoted_reference,
       artifact.manifest_digest
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.runtime_environment_versions version
  ON version.id = environment.current_version_id
JOIN control_plane.image_artifacts artifact
  ON artifact.id = version.role_image_artifact_id
JOIN control_plane.role_image_recipes recipe
  ON recipe.id = artifact.recipe_id
WHERE environment.organization_id = @organization_id::uuid
  AND environment.project_id = @project_id::uuid
  AND environment.name = 'i18n:DEFAULT_RUNTIME_ENVIRONMENT'
  AND environment.state = 'ACTIVE'
  AND recipe.project_id = environment.project_id
  AND recipe.state = 'ACTIVE'
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.promotion_state = 'PROMOTED'
  AND artifact.promoted_reference <> ''
LIMIT 1
