-- name: prewarm_pool__update :exec
UPDATE runtime_manager_prewarm_pools
SET
    scope_type = @scope_type,
    scope_id = @scope_id,
    runtime_profile = @runtime_profile,
    fleet_scope_id = @fleet_scope_id::uuid,
    target_size = @target_size,
    status = @status,
    last_capacity_status = @last_capacity_status,
    updated_at = @updated_at,
    version = @version
WHERE id = @id
  AND version = @previous_version;
