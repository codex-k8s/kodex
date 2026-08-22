-- name: platform__permissions_projectidbyresource_select_owner_gates_organization_id_ref :many
SELECT project_id::text FROM control_plane.owner_gates WHERE organization_id=$1::uuid AND ref=$2
