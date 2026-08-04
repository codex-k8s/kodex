-- name: InvocationGet
SELECT invocation.payload, approval.payload, result.payload
  FROM integration_gateway.invocations AS invocation
  LEFT JOIN integration_gateway.approvals AS approval ON approval.invocation_id = invocation.invocation_id
  LEFT JOIN integration_gateway.results AS result ON result.invocation_id = invocation.invocation_id
 WHERE invocation.invocation_id = @invocation_id
   AND invocation.tenant_id = @tenant_id AND invocation.project_id = @project_id
