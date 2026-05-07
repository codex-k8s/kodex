-- name: workspace_materialization__get :one
SELECT
    id,
    slot_id,
    status,
    policy_digest,
    sources_json,
    fingerprint,
    started_at,
    finished_at,
    last_error_code,
    last_error_message,
    version,
    created_at,
    updated_at
FROM runtime_manager_workspace_materializations
WHERE id = @id;
