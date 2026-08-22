-- name: platform__commands_mustprojectid_select_projects_organization_id_ref_lifecycle :one
SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2 AND lifecycle='ACTIVE'
