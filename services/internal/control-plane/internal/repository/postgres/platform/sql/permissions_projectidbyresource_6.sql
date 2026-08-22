-- name: platform__permissions_projectidbyresource_6 :many
SELECT project_id::text FROM control_plane.owner_gates WHERE organization_id=$1::uuid AND ref=$2
