SELECT binding_id, binding_version, state, fetched_commit, source_revision,
       source_sha256, encrypted_snapshot, target_resource_id, target_version,
       target_sha256, command_intent_sha256, receipt_id, receipt_sha256,
       failure_category, updated_at
  FROM integration_gateway.git_reconciliations
 WHERE reconciliation_id = @reconciliation_id AND tenant_id = @tenant_id AND project_id = @project_id
