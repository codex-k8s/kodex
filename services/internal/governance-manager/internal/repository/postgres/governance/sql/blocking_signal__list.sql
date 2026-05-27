-- name: blocking_signal__list :many
SELECT
    id, target_type, target_ref, source_type, source_ref, severity,
    summary, status, version, created_at, updated_at, resolved_at
FROM governance_manager_blocking_signals
WHERE (@target_type::text = '' OR target_type = @target_type)
  AND (@target_ref::text = '' OR target_ref = @target_ref)
  AND (@status::text = '' OR status = @status)
  AND (@severity::text = '' OR severity = @severity)
ORDER BY created_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
