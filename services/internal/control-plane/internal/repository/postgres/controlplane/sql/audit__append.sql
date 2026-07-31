INSERT INTO control_plane.audit_events (
    id,
    organization_id,
    project_id,
    actor_id,
    action,
    resource_id,
    resource_kind,
    resource_version,
    outcome,
    correlation_id,
    policy_revision,
    occurred_at
) VALUES (
    @id::uuid,
    @organization_id::uuid,
    @project_id::uuid,
    @actor_id::uuid,
    @action,
    @resource_id::uuid,
    @resource_kind,
    @resource_version,
    @outcome,
    @correlation_id::uuid,
    @policy_revision,
    @occurred_at
)
