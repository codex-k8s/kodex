SELECT reconciliation.binding_id, reconciliation.binding_version, reconciliation.state,
       reconciliation.fetched_commit, reconciliation.source_revision, reconciliation.source_sha256,
       reconciliation.encrypted_snapshot, reconciliation.target_resource_id,
       reconciliation.target_version, reconciliation.target_sha256,
       reconciliation.command_intent_sha256, reconciliation.receipt_id,
       reconciliation.receipt_sha256, reconciliation.failure_category,
       reconciliation.updated_at
  FROM integration_gateway.git_reconciliations AS reconciliation
 WHERE reconciliation.reconciliation_id = @resource_id
   AND reconciliation.tenant_id = @tenant_id AND reconciliation.project_id = @project_id
