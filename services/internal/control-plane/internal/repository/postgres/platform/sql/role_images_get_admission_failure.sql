-- name: role_images_get_admission_failure :one
SELECT artifact.ref, artifact.version, recipe.ref, artifact.recipe_generation,
       build.ref, artifact.build_attempt, artifact.scope_kind,
       (SELECT ref FROM control_plane.organizations WHERE id = artifact.organization_id),
       COALESCE(project.ref, ''), 'FAILED', artifact.admission_failure_code
FROM control_plane.image_artifacts artifact
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
JOIN control_plane.image_builds build ON build.id = artifact.build_id
LEFT JOIN control_plane.projects project ON project.id = artifact.project_id
WHERE artifact.organization_id = @organization_id::uuid AND artifact.recipe_id = @recipe_id::uuid
  AND recipe.organization_id = artifact.organization_id AND recipe.state = 'ACTIVE'
  AND artifact.recipe_version = recipe.version AND artifact.recipe_generation = recipe.generation
  AND artifact.spec_sha256 = recipe.spec_sha256
  AND artifact.policy_revision = recipe.policy_revision AND artifact.policy_sha256 = recipe.policy_sha256
  AND artifact.role_runtime_contract_revision = recipe.role_runtime_contract_revision
  AND artifact.role_runtime_contract_sha256 = recipe.role_runtime_contract_sha256
  AND artifact.admission_state = 'FAILED' AND artifact.admission_failure_code <> ''
  AND build.id = (
      SELECT latest.id FROM control_plane.image_builds latest
      WHERE latest.organization_id = artifact.organization_id AND latest.recipe_id = artifact.recipe_id
      ORDER BY latest.created_at DESC, latest.updated_at DESC, latest.attempt DESC, latest.ref DESC LIMIT 1
  )
