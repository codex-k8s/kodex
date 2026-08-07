-- name: ApprovalLock
SELECT approval.payload, approval.version, approval.status, approval.expires_at,
       invocation.payload, invocation.status, clock_timestamp()
  FROM integration_gateway.approvals AS approval
  JOIN integration_gateway.invocations AS invocation ON invocation.invocation_id = approval.invocation_id
 WHERE approval.approval_id = @approval_id
   AND approval.tenant_id = @tenant_id AND approval.project_id = @project_id
 FOR UPDATE OF approval, invocation
