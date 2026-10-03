-- name: organization_environment_binding_reparent :exec
UPDATE control_plane.agent_runtime_environment_bindings
SET environment_set_id=(SELECT id FROM control_plane.runtime_environment_sets WHERE ref=$2), environment_version_id=NULL
WHERE agent_id=(SELECT id FROM control_plane.agents WHERE ref=$1);
