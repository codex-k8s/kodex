-- name: runtime_managed_mcp__current_grants :one
SELECT count(*)
FROM control_plane.integration_grants binding
JOIN control_plane.integration_connections connection ON connection.id=binding.connection_id AND connection.organization_id=binding.organization_id
JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
JOIN control_plane.agents agent ON agent.ref=binding.target_ref AND agent.organization_id=binding.organization_id
WHERE binding.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
  AND connection.version=@connection_version AND connection.lifecycle_state='ACTIVE' AND connection.enabled
  AND connection.state='CONNECTED' AND agent.ref=@agent_ref AND agent.enabled
  AND connection.definition_key='context7' AND definition.enabled AND definition.adapter_readiness='READY'
  AND definition.adapter_owner='integration-gateway' AND definition.execution_route='MANAGED_MCP'
  AND connection.definition_version=@definition_version AND connection.definition_digest=@definition_digest
  AND definition.definition_version=connection.definition_version AND definition.digest=connection.definition_digest
  AND binding.enabled AND binding.target_kind='AGENT' AND binding.risk='READ' AND binding.approval_policy='NONE'
  AND binding.definition_version=@definition_version AND binding.definition_digest=@definition_digest
  AND ((binding.ref=@resolve_ref AND binding.version=@resolve_version AND binding.capability_key='context7.library.resolve')
       OR (binding.ref=@query_ref AND binding.version=@query_version AND binding.capability_key='context7.docs.query'));
