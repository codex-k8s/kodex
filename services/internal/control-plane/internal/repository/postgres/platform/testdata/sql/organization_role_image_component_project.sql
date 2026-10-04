-- name: organization_role_image_component_project :one
SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2;
