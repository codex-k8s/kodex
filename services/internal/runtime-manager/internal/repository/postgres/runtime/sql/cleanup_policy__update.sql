-- name: cleanup_policy__update :exec
UPDATE runtime_manager_cleanup_policies
SET
    scope_type = @scope_type,
    scope_id = @scope_id,
    ttl_seconds = @ttl_seconds,
    failed_ttl_seconds = @failed_ttl_seconds,
    keep_short_log_tail = @keep_short_log_tail,
    status = @status,
    updated_at = @updated_at,
    version = @version
WHERE id = @id
  AND version = @previous_version;
