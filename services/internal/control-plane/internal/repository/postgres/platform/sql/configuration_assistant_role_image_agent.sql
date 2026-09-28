-- name: configuration_assistant_role_image_agent :one
SELECT role.ref, agent.version
FROM control_plane.agents agent
JOIN control_plane.projects project ON project.id = agent.project_id
JOIN control_plane.role_definitions role ON role.id = agent.role_definition_id
WHERE agent.organization_id = $1::uuid
  AND project.ref = $2
  AND project.lifecycle = 'ACTIVE'
  AND agent.ref = $3
  AND agent.system_key IS NULL
  AND agent.state <> 'ARCHIVED'
  AND role.lifecycle = 'ACTIVE'
