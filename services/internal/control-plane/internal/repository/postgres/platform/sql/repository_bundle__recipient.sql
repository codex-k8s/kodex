-- name: repository_bundle__recipient :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.agents agent
    JOIN control_plane.projects project ON project.id=agent.project_id AND project.organization_id=agent.organization_id
    WHERE agent.organization_id=@organization_id::uuid AND agent.ref=@agent_ref
      AND agent.system_key IS NULL AND agent.enabled AND agent.state='READY' AND project.lifecycle='ACTIVE'
      AND NOT EXISTS (SELECT 1 FROM control_plane.project_assistant_profiles profile
                      WHERE profile.organization_id=agent.organization_id AND profile.agent_id=agent.id)
);
