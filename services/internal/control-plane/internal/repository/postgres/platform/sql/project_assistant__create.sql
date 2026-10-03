-- name: project_assistant__create :one
WITH inserted AS (
    INSERT INTO control_plane.project_assistant_profiles(ref, organization_id, project_id, agent_id, created_by)
    SELECT @profile_ref, @organization_id::uuid, @project_id::uuid, agent.id, @created_by::uuid
    FROM control_plane.agents agent
    WHERE agent.organization_id = @organization_id::uuid AND agent.project_id = @project_id::uuid
      AND agent.ref = @agent_ref AND agent.system_key IS NULL AND agent.enabled AND agent.state <> 'ARCHIVED'
    RETURNING ref, project_id, agent_id, version, created_at, updated_at
)
SELECT inserted.ref, project.ref, agent.ref, agent.name, 'ACTIVE',
       inserted.version, inserted.created_at, inserted.updated_at
FROM inserted
JOIN control_plane.projects project ON project.id = inserted.project_id
JOIN control_plane.agents agent ON agent.id = inserted.agent_id;
