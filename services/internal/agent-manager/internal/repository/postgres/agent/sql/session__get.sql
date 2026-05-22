-- name: session__get :one
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
WHERE id = @id;
