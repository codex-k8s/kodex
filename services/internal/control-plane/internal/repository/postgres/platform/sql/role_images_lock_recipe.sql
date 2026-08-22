-- name: role_images_lock_recipe :one
SELECT recipe.id::text, recipe.project_id::text, project.ref, recipe.role_definition_id::text,
       role.ref, recipe.name, recipe.state, recipe.specification, recipe.generation,
       recipe.spec_sha256, recipe.policy_revision, recipe.policy_sha256,
       recipe.role_runtime_contract_revision, recipe.role_runtime_contract_sha256,
       COALESCE(recipe.active_image_artifact_id::text, ''), recipe.version,
       recipe.created_at, recipe.updated_at
FROM control_plane.role_image_recipes recipe
JOIN control_plane.projects project ON project.id = recipe.project_id
JOIN control_plane.role_definitions role ON role.id = recipe.role_definition_id
WHERE recipe.organization_id = $1::uuid
  AND recipe.ref = $2
  AND (
      $3 IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships membership
          WHERE membership.project_id = project.id
            AND membership.subject_id = $4::uuid
            AND membership.active
            AND 'MANAGE_AGENTS' = ANY(membership.permissions)
      )
  )
FOR UPDATE OF recipe
