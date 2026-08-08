SELECT authorization.payload, connection.payload, clock_timestamp()
  FROM integration_gateway.provider_authorization_attempts AS authorization
  JOIN integration_gateway.managed_provider_connections AS connection
    ON connection.connection_id = authorization.connection_id
 WHERE authorization.authorization_id = @authorization_id
   AND authorization.tenant_id = @tenant_id AND authorization.project_id = @project_id
 FOR UPDATE OF authorization, connection
