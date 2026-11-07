-- name: personal_launch__requirements :many
SELECT DISTINCT connection.ref,grant_row.capability_key,connection.version,
       grant_row.enabled AND grant_row.definition_version=connection.definition_version
       AND grant_row.definition_digest=connection.definition_digest AS grant_current
FROM jsonb_to_recordset(@targets::jsonb) AS target(agent_ref text,capability_keys text[],all_capabilities boolean)
JOIN control_plane.agents agent ON agent.organization_id=@organization_id::uuid
 AND agent.ref=target.agent_ref
 AND ((NOT @system_assistant::boolean AND agent.project_id=NULLIF(@project_id,'')::uuid) OR
      (@system_assistant::boolean AND agent.project_id IS NULL
       AND agent.system_key='system-assistant' AND EXISTS (
         SELECT 1 FROM control_plane.assistant_runtime runtime
         WHERE runtime.organization_id=agent.organization_id AND runtime.agent_id=agent.id)))
JOIN control_plane.integration_grants grant_row ON grant_row.organization_id=agent.organization_id
 AND grant_row.target_kind='AGENT' AND grant_row.target_ref=agent.ref
 AND (NOT target.all_capabilities OR grant_row.enabled)
 AND (target.all_capabilities OR grant_row.capability_key=ANY(target.capability_keys))
JOIN control_plane.integration_connections connection ON connection.id=grant_row.connection_id
 AND connection.organization_id=grant_row.organization_id
WHERE connection.credential_policy='INITIATOR_REQUIRED'
ORDER BY connection.ref,grant_row.capability_key,connection.version,grant_current
LIMIT 257;
