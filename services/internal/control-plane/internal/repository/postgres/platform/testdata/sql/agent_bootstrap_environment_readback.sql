-- name: agent_bootstrap_environment_readback :one
SELECT md5(row_to_json(environment)::text),
       md5(row_to_json(revision)::text),
       md5(row_to_json(binding)::text),
       (SELECT count(*) FROM control_plane.runtime_environment_versions item
        WHERE item.environment_set_id = environment.id)
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id = agent.id
JOIN control_plane.runtime_environment_sets environment ON environment.id = binding.environment_set_id
JOIN control_plane.runtime_environment_versions revision ON revision.id = environment.current_version_id
WHERE agent.ref = $1;
