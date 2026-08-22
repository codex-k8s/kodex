-- name: platform__permissions_projectidbyresource_2 :many
SELECT project_id::text FROM control_plane.agents WHERE organization_id=$1::uuid AND ref=$2 AND project_id IS NOT NULL
