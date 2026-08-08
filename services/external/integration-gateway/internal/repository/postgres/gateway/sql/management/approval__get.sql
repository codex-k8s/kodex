SELECT payload, version
  FROM integration_gateway.approvals
 WHERE approval_id = @approval_id AND tenant_id = @tenant_id AND project_id = @project_id
