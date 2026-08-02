-- name: ToolsList
SELECT DISTINCT definition.payload, connection.payload, grant.payload
  FROM integration_gateway.transport_sessions AS session
  JOIN integration_gateway.grants AS grant
    ON grant.tenant_id = session.tenant_id AND grant.project_id = session.project_id
   AND grant.session_id = session.agent_session_id AND grant.turn_id = session.turn_id
   AND grant.attempt = session.attempt AND grant.input_digest = session.input_digest
   AND grant.runtime_revision_id = session.runtime_revision_id
  JOIN integration_gateway.connections AS connection
    ON connection.connection_id = grant.connection_id
   AND connection.tenant_id = session.tenant_id AND connection.project_id = session.project_id
  JOIN integration_gateway.definitions AS definition
    ON definition.definition_id = connection.definition_id
   AND definition.definition_version = connection.definition_version
 WHERE session.transport_session_id = @transport_session_id
   AND session.tenant_id = @tenant_id AND session.project_id = @project_id
   AND session.status IN ('INITIALIZING', 'ACTIVE') AND session.expires_at > clock_timestamp()
   AND grant.status = 'ACTIVE' AND grant.expires_at > clock_timestamp()
   AND grant.generation = session.grant_generation
   AND connection.status = 'VALID'
   AND connection.updated_at > clock_timestamp() - interval '5 minutes'
 ORDER BY definition.payload::text, connection.payload::text, grant.payload::text
