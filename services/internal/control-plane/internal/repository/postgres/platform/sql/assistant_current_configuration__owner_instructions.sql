-- name: assistant_current_configuration__owner_instructions :one
SELECT runtime.core_prompt_revision, runtime.owner_instructions, runtime.version
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent ON agent.id = runtime.agent_id
WHERE agent.organization_id = @organization_id::uuid
  AND agent.ref = @agent_ref AND agent.system_key = 'system-assistant'
  AND agent.project_id IS NULL AND agent.enabled AND agent.state <> 'ARCHIVED';
