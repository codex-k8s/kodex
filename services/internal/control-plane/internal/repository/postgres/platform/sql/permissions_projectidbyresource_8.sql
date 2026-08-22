-- name: platform__permissions_projectidbyresource_8 :many
SELECT project_id::text FROM control_plane.schedules WHERE organization_id=$1::uuid AND ref=$2
