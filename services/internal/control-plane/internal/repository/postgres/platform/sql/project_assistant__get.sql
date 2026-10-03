-- name: project_assistant__get :one
SELECT profile.ref, project.ref, agent.ref, agent.name,
       CASE WHEN agent.state = 'ARCHIVED' THEN 'ARCHIVED'
            WHEN NOT agent.enabled THEN 'DISABLED' ELSE 'ACTIVE' END,
       profile.version, profile.created_at, profile.updated_at
FROM control_plane.project_assistant_profiles profile
JOIN control_plane.projects project
  ON project.id = profile.project_id AND project.organization_id = profile.organization_id
JOIN control_plane.agents agent
  ON agent.id = profile.agent_id AND agent.organization_id = profile.organization_id
 AND agent.project_id = profile.project_id AND agent.system_key IS NULL
WHERE profile.organization_id = @organization_id::uuid AND project.ref = @project_ref
  AND project.lifecycle = 'ACTIVE'
  AND (@authority_project = '' OR project.id = NULLIF(@authority_project, '')::uuid);
