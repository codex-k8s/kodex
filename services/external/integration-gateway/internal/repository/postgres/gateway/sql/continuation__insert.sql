-- name: ContinuationInsert
INSERT INTO integration_gateway.continuation_effects (
    invocation_id, tenant_id, project_id, action, desired_action,
    application_grant_expires_at, available_at, payload, updated_at
) VALUES (
    @invocation_id, @tenant_id, @project_id, @action, @desired_action,
    @application_grant_expires_at, @available_at, @payload::jsonb, @updated_at
)
