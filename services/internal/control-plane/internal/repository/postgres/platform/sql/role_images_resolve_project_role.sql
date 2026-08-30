-- name: role_images_resolve_project_role :one
SELECT project.id::text, role.id::text
FROM control_plane.projects project
JOIN control_plane.role_definitions role
  ON role.organization_id = project.organization_id
 AND role.project_id = project.id
WHERE project.organization_id = $1::uuid
  AND project.ref = $2
  AND project.lifecycle = 'ACTIVE'
  AND role.ref = $3
  AND role.lifecycle = 'ACTIVE'
