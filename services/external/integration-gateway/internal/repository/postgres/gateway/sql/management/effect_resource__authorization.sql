SELECT authorization.payload
  FROM integration_gateway.provider_authorization_attempts AS authorization
 WHERE authorization.authorization_id = @resource_id
   AND authorization.tenant_id = @tenant_id AND authorization.project_id = @project_id
