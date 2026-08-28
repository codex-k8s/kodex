-- name: runtime_completeexecution_select_agent_capability :one
SELECT $2::text=ANY(agent.capabilities)
FROM control_plane.run_nodes node
JOIN control_plane.agents agent
  ON agent.id=node.agent_id
 AND agent.organization_id=node.organization_id
WHERE node.organization_id=$1::uuid
  AND node.id=$3::uuid
