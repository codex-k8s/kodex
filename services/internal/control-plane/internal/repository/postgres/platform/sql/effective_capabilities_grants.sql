-- name: effective_capabilities_grants :many
SELECT grant_row.ref,grant_row.version,grant_row.capability_key,
 connection.ref,connection.version,connection.definition_key,
 connection.definition_version,connection.definition_digest,connection.name,
 grant_row.enabled AND connection.enabled AND connection.state='CONNECTED'
 AND connection.lifecycle_state='ACTIVE'
 AND definition.enabled AND definition.adapter_readiness='READY'
 AND (definition.adapter_owner,definition.execution_route) IN
 (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
 AND grant_row.definition_version=connection.definition_version
 AND grant_row.definition_digest=connection.definition_digest AS eligible
FROM control_plane.integration_grants grant_row
JOIN control_plane.integration_connections connection ON connection.id=grant_row.connection_id
 AND connection.organization_id=grant_row.organization_id
JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
WHERE grant_row.organization_id=$1::uuid AND grant_row.target_kind='AGENT'
 AND grant_row.target_ref=$2
ORDER BY connection.ref,grant_row.capability_key,grant_row.ref
LIMIT 4097;
