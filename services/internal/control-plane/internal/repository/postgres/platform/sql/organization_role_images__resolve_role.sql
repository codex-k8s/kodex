-- name: organization_role_images__resolve_role :one
SELECT agent.role_definition_id::text
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent ON agent.id=runtime.agent_id AND agent.organization_id=runtime.organization_id
JOIN control_plane.role_definitions role ON role.id=agent.role_definition_id AND role.organization_id=agent.organization_id
WHERE runtime.organization_id=$1::uuid AND agent.system_key='system-assistant' AND agent.project_id IS NULL;
