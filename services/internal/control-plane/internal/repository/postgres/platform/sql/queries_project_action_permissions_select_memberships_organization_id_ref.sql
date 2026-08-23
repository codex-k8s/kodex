-- name: queries_project_action_permissions_select_memberships_organization_id_ref :one
SELECT membership.permissions
FROM control_plane.projects project
JOIN control_plane.memberships membership
  ON membership.project_id = project.id
 AND membership.subject_id = $3::uuid
 AND membership.active
WHERE project.organization_id = $1::uuid
  AND project.ref = $2
