-- name: cleanup_policy__list_active :many
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
WHERE status = 'active'
  AND (@id::uuid IS NULL OR id = @id::uuid)
ORDER BY
    CASE scope_type
        WHEN 'repository' THEN 1
        WHEN 'project' THEN 2
        WHEN 'runtime_profile' THEN 3
        WHEN 'organization' THEN 4
        ELSE 5
    END,
    updated_at DESC,
    id ASC;
