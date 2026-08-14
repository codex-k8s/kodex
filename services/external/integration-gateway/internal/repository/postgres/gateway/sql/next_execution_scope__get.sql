-- name: NextExecutionScopeGet
SELECT tenant_id, project_id
  FROM integration_gateway.next_execution_scope()
 WHERE tenant_id IS NOT NULL
   AND project_id IS NOT NULL
