-- name: project_assistant__resolve_candidate :one
SELECT agent.id::text, agent.ref, @assistant_scope::text,
       COALESCE(profile.ref, ''), COALESCE(project.ref, '')
FROM control_plane.agents agent
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.agent_id = agent.id AND profile.organization_id = agent.organization_id
LEFT JOIN control_plane.projects project
  ON project.id = profile.project_id AND project.organization_id = agent.organization_id
WHERE agent.organization_id = @organization_id::uuid AND agent.enabled AND agent.state <> 'ARCHIVED'
  AND ((@assistant_scope = 'SYSTEM' AND agent.system_key = 'system-assistant' AND agent.project_id IS NULL)
    OR (@assistant_scope = 'PROJECT' AND agent.system_key IS NULL AND agent.project_id = profile.project_id
        AND project.ref = @project_ref AND project.lifecycle = 'ACTIVE'
        AND (@authority_project = '' OR project.id = NULLIF(@authority_project, '')::uuid)));
