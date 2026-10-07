-- name: environment_initial_system_binding_readback :one
SELECT binding.environment_version_id IS NULL
FROM control_plane.agent_runtime_environment_bindings binding
JOIN control_plane.agents agent ON agent.id=binding.agent_id
WHERE agent.ref=$1;
