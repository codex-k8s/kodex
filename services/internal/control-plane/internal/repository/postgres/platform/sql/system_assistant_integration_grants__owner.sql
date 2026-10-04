-- name: system_assistant_integration_grants__owner :one
SELECT agent.ref, agent.name, agent.version, agent.enabled AND agent.state='READY'
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent ON agent.id=runtime.agent_id AND agent.organization_id=runtime.organization_id
WHERE runtime.organization_id=$1::uuid AND agent.system_key='system-assistant'
  AND agent.project_id IS NULL AND agent.state<>'ARCHIVED';
