-- name: cleanup_policy__get :one
SELECT
    id,
    scope_type,
    scope_id,
    ttl_seconds,
    failed_ttl_seconds,
    keep_short_log_tail,
    status,
    created_at,
    updated_at,
    version
FROM runtime_manager_cleanup_policies
WHERE id = @id;
