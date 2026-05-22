-- name: session__update :exec
UPDATE agent_manager_sessions
SET
    scope_type = @scope_type,
    scope_ref = @scope_ref,
    provider_work_item_ref = @provider_work_item_ref,
    flow_version_id = @flow_version_id::uuid,
    current_stage_id = @current_stage_id::uuid,
    latest_state_snapshot_id = @latest_state_snapshot_id::uuid,
    status = @status,
    created_by_actor_ref = @created_by_actor_ref,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
