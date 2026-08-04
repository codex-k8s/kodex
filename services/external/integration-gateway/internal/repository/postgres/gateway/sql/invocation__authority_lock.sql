-- name: InvocationAuthorityLock
SELECT true
  FROM integration_gateway.transport_sessions AS session
  JOIN integration_gateway.grants AS grant
    ON grant.grant_id = @grant_id
   AND grant.tenant_id = session.tenant_id AND grant.project_id = session.project_id
   AND grant.session_id = session.agent_session_id AND grant.turn_id = session.turn_id
   AND grant.attempt = session.attempt AND grant.input_digest = session.input_digest
   AND grant.runtime_revision_id = session.runtime_revision_id
  JOIN integration_gateway.connections AS connection
    ON connection.connection_id = @connection_id
   AND connection.connection_id = grant.connection_id
   AND connection.tenant_id = session.tenant_id AND connection.project_id = session.project_id
 WHERE session.transport_session_id = @transport_session_id
   AND session.tenant_id = @tenant_id AND session.project_id = @project_id
   AND session.status IN ('INITIALIZING', 'ACTIVE')
   AND session.expires_at > clock_timestamp()
   AND session.grant_generation = @grant_generation
   AND grant.status = 'ACTIVE' AND grant.expires_at > clock_timestamp()
   AND grant.generation = @grant_generation
   AND connection.status = 'VALID'
   AND connection.generation = @connection_generation
 FOR UPDATE OF session, grant, connection
