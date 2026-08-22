-- name: commands_createagent_select_role_definitions_organization_id_project_id_ref :one
SELECT id::text, ref, name
FROM control_plane.role_definitions
WHERE organization_id = $1::uuid
  AND project_id = $2::uuid
  AND ref = $3
  AND lifecycle = 'ACTIVE'
