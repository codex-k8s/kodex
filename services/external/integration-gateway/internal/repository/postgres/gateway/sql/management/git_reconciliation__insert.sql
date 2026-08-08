INSERT INTO integration_gateway.git_reconciliations (
    reconciliation_id, tenant_id, project_id, binding_id, binding_version,
    state, command_intent_sha256, receipt_id, receipt_sha256, created_at, updated_at
) VALUES (
    @reconciliation_id, @tenant_id, @project_id, @binding_id, @binding_version,
    @state, @command_intent_sha256, @receipt_id, @receipt_sha256, @created_at, @updated_at
)
