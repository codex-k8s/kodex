-- name: prewarm_pool__insert :exec
INSERT INTO runtime_manager_prewarm_pools (
    id,
    scope_type,
    scope_id,
    runtime_profile,
    fleet_scope_id,
    target_size,
    status,
    last_capacity_status,
    created_at,
    updated_at,
    version
) VALUES (
    @id,
    @scope_type,
    @scope_id,
    @runtime_profile,
    @fleet_scope_id::uuid,
    @target_size,
    @status,
    @last_capacity_status,
    @created_at,
    @updated_at,
    @version
);
