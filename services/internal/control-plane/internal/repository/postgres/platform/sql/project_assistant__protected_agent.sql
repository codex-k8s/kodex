-- name: project_assistant__protected_agent :one
SELECT COALESCE(agent.system_key,'')='system-assistant' OR EXISTS (
    SELECT 1 FROM control_plane.project_assistant_profiles profile
    WHERE profile.organization_id=agent.organization_id AND profile.agent_id=agent.id
)
FROM control_plane.agents agent
WHERE agent.organization_id=$1::uuid AND agent.ref=$2;
