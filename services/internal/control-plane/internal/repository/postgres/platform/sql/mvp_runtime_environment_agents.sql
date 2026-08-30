-- name: mvp_runtime_environment_agents :many
SELECT agent.ref
FROM control_plane.agent_runtime_environment_bindings binding
JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
JOIN control_plane.agents agent ON agent.id = binding.agent_id
WHERE environment.organization_id = @organization_id::uuid
  AND environment.ref = @environment_ref
  AND agent.state <> 'ARCHIVED'
  AND (@cursor_ref = '' OR agent.ref > @cursor_ref)
ORDER BY agent.ref
LIMIT @page_size;
