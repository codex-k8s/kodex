-- name: cleanup_policy__insert :exec
INSERT INTO runtime_manager_cleanup_policies (
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
) VALUES (
    @id,
    @scope_type,
    @scope_id,
    @ttl_seconds,
    @failed_ttl_seconds,
    @keep_short_log_tail,
    @status,
    @created_at,
    @updated_at,
    @version
);
