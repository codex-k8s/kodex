-- name: assistant_agent_runtime__resolve_target :one
SELECT 'PROJECT', project.ref, '', agent.name, agent.version
FROM control_plane.agents agent
JOIN control_plane.projects project ON project.id = agent.project_id AND project.organization_id = agent.organization_id
WHERE agent.organization_id = @organization_id::uuid AND agent.ref = @agent_ref
  AND project.ref = @project_ref AND project.lifecycle = 'ACTIVE'
  AND agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
  AND (@authority_project = '' OR agent.project_id = NULLIF(@authority_project, '')::uuid)
  AND NOT EXISTS (SELECT 1 FROM control_plane.project_assistant_profiles profile
                  WHERE profile.organization_id = agent.organization_id AND profile.agent_id = agent.id)
FOR UPDATE OF agent;
