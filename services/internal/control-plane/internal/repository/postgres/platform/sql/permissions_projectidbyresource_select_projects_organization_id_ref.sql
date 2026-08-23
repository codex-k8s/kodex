-- name: permissions_projectidbyresource_select_projects_organization_id_ref :many
SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2
