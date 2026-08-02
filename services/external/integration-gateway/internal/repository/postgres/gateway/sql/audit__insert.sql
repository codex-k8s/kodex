-- name: AuditInsert
INSERT INTO integration_gateway.audit_events (
    audit_id, tenant_id, project_id, actor_id, action, resource_kind,
    resource_id, request_hash, outcome, reason_code, occurred_at
) VALUES (
    @audit_id, @tenant_id, @project_id, @actor_id, @action, @resource_kind,
    @resource_id, @request_hash, @outcome, @reason_code, @occurred_at
)
