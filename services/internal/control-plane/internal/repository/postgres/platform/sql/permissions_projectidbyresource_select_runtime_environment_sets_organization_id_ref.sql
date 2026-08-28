-- name: permissions_projectidbyresource_select_runtime_environment_sets_organization_id_ref :one
SELECT project_id::text
FROM control_plane.runtime_environment_sets
WHERE organization_id = $1::uuid AND ref = $2 AND project_id IS NOT NULL;
