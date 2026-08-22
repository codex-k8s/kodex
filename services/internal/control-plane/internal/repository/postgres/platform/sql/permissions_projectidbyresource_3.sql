-- name: platform__permissions_projectidbyresource_3 :many
SELECT project_id::text FROM control_plane.workflows WHERE organization_id=$1::uuid AND ref=$2
