-- name: InvocationCancelLock
SELECT invocation.payload, invocation.status, invocation.expires_at,
       COALESCE(approval.payload, '{}'::jsonb), clock_timestamp()
  FROM integration_gateway.invocations AS invocation
  LEFT JOIN integration_gateway.approvals AS approval
    ON approval.invocation_id = invocation.invocation_id
 WHERE invocation.invocation_id = @invocation_id
   AND invocation.tenant_id = @tenant_id AND invocation.project_id = @project_id
   AND (@transport_session_id = '' OR invocation.transport_session_id = @transport_session_id)
 FOR UPDATE OF invocation
