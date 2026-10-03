-- name: organization_environment_reparent :exec
UPDATE control_plane.runtime_environment_sets
SET project_id=$2::uuid, scope_kind='PROJECT'
WHERE ref=$1;
