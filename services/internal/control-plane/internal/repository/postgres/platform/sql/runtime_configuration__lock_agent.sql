-- name: runtime_configuration__lock_agent :one
SELECT agent.id::text,
       COALESCE(agent.project_id::text, ''),
       COALESCE(project.ref, ''),
       agent.version,
       config.version_number,
       overlay_version.id::text,
       overlay_version.version_number,
       binding.version
       ,config.runtime_profile_key
FROM control_plane.agents agent
LEFT JOIN control_plane.projects project ON project.id = agent.project_id
JOIN control_plane.agent_runtime_config_versions config ON config.id = agent.current_runtime_config_id
JOIN control_plane.agent_config_overlay_versions overlay_version ON overlay_version.id = agent.current_config_overlay_id
JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id = agent.id
WHERE agent.organization_id = $1::uuid AND agent.ref = $2
FOR UPDATE OF agent;
