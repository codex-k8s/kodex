-- name: runtime_configuration__count_environment_bindings :one
SELECT count(*)
FROM control_plane.agent_runtime_environment_bindings
WHERE environment_set_id = @environment_id::uuid;
