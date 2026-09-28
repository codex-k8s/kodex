-- name: configuration_hydrateassistantoperation_select_agent_capability :one
SELECT agent.name,
       agent.version,
       agent.capabilities,
       EXISTS (
           SELECT 1
           FROM control_plane.platform_capabilities capability
           WHERE capability.stable_key = $4
             AND capability.enabled
       ) AS capability_exists
FROM control_plane.agents agent
JOIN control_plane.projects project ON project.id = agent.project_id
WHERE agent.organization_id = $1::uuid
  AND project.ref = $2
  AND project.lifecycle = 'ACTIVE'
  AND agent.ref = $3
  AND agent.system_key IS NULL
  AND agent.state <> 'ARCHIVED'
