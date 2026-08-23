-- name: role_images_list_recipes :many
SELECT recipe.ref, project.ref, role.ref, recipe.name, recipe.state, recipe.specification,
       recipe.generation, recipe.spec_sha256, recipe.policy_revision, recipe.policy_sha256,
       recipe.role_runtime_contract_revision, recipe.role_runtime_contract_sha256,
       COALESCE(artifact.ref, ''), COALESCE(artifact.promoted_reference, ''),
       recipe.version, recipe.created_at, recipe.updated_at,
       (
           $4 IN ('OWNER', 'ADMINISTRATOR')
           OR EXISTS (
               SELECT 1
               FROM control_plane.memberships membership
               WHERE membership.project_id = project.id
                 AND membership.subject_id = $5::uuid
                 AND membership.active
                 AND 'MANAGE_AGENTS' = ANY(membership.permissions)
           )
       ) AS can_manage
FROM control_plane.role_image_recipes recipe
JOIN control_plane.projects project ON project.id = recipe.project_id
JOIN control_plane.role_definitions role ON role.id = recipe.role_definition_id
LEFT JOIN control_plane.image_artifacts artifact ON artifact.id = recipe.active_image_artifact_id
WHERE recipe.organization_id = $1::uuid
  AND project.ref = $2
  AND ($3 = '' OR role.ref = $3)
  AND (
      $4 IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships membership
          WHERE membership.project_id = project.id
            AND membership.subject_id = $5::uuid
            AND membership.active
            AND 'VIEW' = ANY(membership.permissions)
      )
  )
ORDER BY recipe.updated_at DESC, recipe.ref
LIMIT $6
