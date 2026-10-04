-- name: assistant_configuration__resolve_target :one
SELECT CASE WHEN agent.system_key = 'system-assistant' THEN 'ORGANIZATION' ELSE 'PROJECT' END,
       COALESCE(project.ref, ''), COALESCE(profile.ref, ''), agent.name, agent.version
FROM control_plane.agents agent
LEFT JOIN control_plane.assistant_runtime runtime
  ON runtime.agent_id = agent.id AND runtime.organization_id = agent.organization_id
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.agent_id = agent.id AND profile.organization_id = agent.organization_id
 AND profile.project_id = agent.project_id
LEFT JOIN control_plane.projects project ON project.id = agent.project_id
WHERE agent.organization_id = @organization_id::uuid AND agent.ref = @agent_ref
  AND agent.state <> 'ARCHIVED'
  AND ((agent.system_key = 'system-assistant' AND agent.project_id IS NULL AND runtime.agent_id IS NOT NULL
        AND @authority_project = '')
    OR (agent.system_key IS NULL AND profile.id IS NOT NULL AND project.lifecycle = 'ACTIVE'
        AND (@authority_project = '' OR agent.project_id = NULLIF(@authority_project, '')::uuid)))
FOR UPDATE OF agent;
