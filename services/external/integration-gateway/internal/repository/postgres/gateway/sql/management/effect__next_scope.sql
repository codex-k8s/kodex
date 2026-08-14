SELECT tenant_id, project_id, actor_id
  FROM integration_gateway.next_management_scope()
 WHERE tenant_id IS NOT NULL
   AND project_id IS NOT NULL
   AND actor_id IS NOT NULL
