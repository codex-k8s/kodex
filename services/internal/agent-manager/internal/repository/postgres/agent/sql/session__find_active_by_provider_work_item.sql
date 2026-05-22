-- name: session__find_active_by_provider_work_item :one
SELECT
    id,
    scope_type,
    scope_ref,
    provider_work_item_ref,
    flow_version_id,
    current_stage_id,
    latest_state_snapshot_id,
    status,
    created_by_actor_ref,
    version,
    created_at,
    updated_at
FROM agent_manager_sessions
WHERE scope_type = @scope_type
  AND scope_ref = @scope_ref
  AND provider_work_item_ref = @provider_work_item_ref
  AND status IN ('open', 'waiting')
ORDER BY updated_at DESC, id DESC
LIMIT 1;
