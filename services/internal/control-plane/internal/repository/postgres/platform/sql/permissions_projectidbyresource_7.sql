-- name: platform__permissions_projectidbyresource_7 :many
SELECT project_id::text FROM control_plane.artifacts WHERE organization_id=$1::uuid AND ref=$2
