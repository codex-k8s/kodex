-- name: platform__permissions_projectidbyresource_1 :many
SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2
