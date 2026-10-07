-- name: runtime_managed_mcp__startup_dependencies :one
SELECT count(*),count(*) FILTER (WHERE
    c.enabled AND c.state='CONNECTED' AND c.lifecycle_state='ACTIVE'
    AND d.enabled AND d.adapter_owner='integration-gateway' AND d.execution_route='MANAGED_MCP'
    AND d.adapter_readiness='READY' AND d.definition_version=c.definition_version AND d.digest=c.definition_digest
    AND c.ref=@connection_ref AND c.version=@connection_version
    AND c.definition_version=@definition_version AND c.definition_digest=@definition_digest
    AND binding.risk='READ' AND binding.approval_policy='NONE'
    AND binding.definition_version=c.definition_version AND binding.definition_digest=c.definition_digest
    AND ((binding.capability_key='context7.library.resolve' AND binding.ref=@resolve_ref AND binding.version=@resolve_version)
      OR (binding.capability_key='context7.docs.query' AND binding.ref=@query_ref AND binding.version=@query_version)))
FROM control_plane.integration_grants binding
JOIN control_plane.integration_connections c ON c.id=binding.connection_id AND c.organization_id=binding.organization_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
WHERE binding.organization_id=@organization_id::uuid AND binding.target_kind='AGENT'
  AND binding.target_ref=@agent_ref AND binding.enabled AND c.definition_key='context7';
