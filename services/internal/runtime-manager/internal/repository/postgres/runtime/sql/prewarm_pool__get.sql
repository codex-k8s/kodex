-- name: prewarm_pool__get :one
SELECT
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
FROM runtime_manager_prewarm_pools
WHERE id = @id;
