-- name: runtime_configuration__resolve_image_artifact :one
SELECT artifact.id::text,
       artifact.ref,
       recipe.ref,
       artifact.recipe_generation,
       artifact.promoted_reference,
       artifact.manifest_digest,
       artifact.specification
FROM control_plane.image_artifacts artifact
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
WHERE artifact.organization_id = @organization_id::uuid
  AND recipe.project_id = @project_id::uuid
  AND artifact.ref = @artifact_ref
  AND recipe.state = 'ACTIVE'
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.promotion_state = 'PROMOTED'
  AND artifact.promoted_reference <> '';
