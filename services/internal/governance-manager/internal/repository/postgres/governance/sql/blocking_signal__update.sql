-- name: blocking_signal__update :exec
UPDATE governance_manager_blocking_signals
SET
    summary = @summary,
    status = @status,
    version = @version,
    updated_at = @updated_at,
    resolved_at = @resolved_at
WHERE id = @id
  AND version = @previous_version;
