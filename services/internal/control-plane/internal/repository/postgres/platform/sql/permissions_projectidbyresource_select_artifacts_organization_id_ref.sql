-- name: permissions_projectidbyresource_select_artifacts_organization_id_ref :many
SELECT project_id::text FROM control_plane.artifacts WHERE organization_id=$1::uuid AND ref=$2
