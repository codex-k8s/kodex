-- name: slot__get :one
SELECT
    id,
    slot_key,
    status,
    runtime_mode,
    is_prewarmed,
    fleet_scope_id,
    cluster_id,
    namespace_name,
    agent_run_id,
    project_id,
    repository_ids_json,
    active_workspace_materialization_id,
    runtime_profile,
    fingerprint,
    lease_owner,
    lease_until,
    last_error_code,
    last_error_message,
    version,
    created_at,
    updated_at
FROM runtime_manager_slots
WHERE id = @id;
