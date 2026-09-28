-- name: configuration_hydrateassistantoperation_select_agent :one
SELECT agent.name, agent.purpose, agent.role_description, agent.avatar_url, agent.version
FROM control_plane.agents agent
JOIN control_plane.projects project ON project.id = agent.project_id
WHERE agent.organization_id = $1::uuid
  AND project.ref = $2
  AND project.lifecycle = 'ACTIVE'
  AND agent.ref = $3
  AND agent.system_key IS NULL
  AND agent.state <> 'ARCHIVED'
