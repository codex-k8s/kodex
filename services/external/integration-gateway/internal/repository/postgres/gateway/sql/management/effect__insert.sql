INSERT INTO integration_gateway.management_effects (
    effect_id, tenant_id, project_id, actor_id, effect_kind, resource_kind, resource_id,
    resource_version, resource_generation, intent_sha256, status, available_at,
    payload, created_at, updated_at
) VALUES (
    @effect_id, @tenant_id, @project_id, @actor_id, @effect_kind, @resource_kind, @resource_id,
    @resource_version, @resource_generation, @intent_sha256, 'PENDING', @available_at,
    @payload::jsonb, @created_at, @updated_at
)
