-- name: outbox_event__mark_permanently_failed :exec
UPDATE governance_manager_outbox_events
SET
    failed_permanently_at = @failed_permanently_at,
    locked_until = NULL,
    last_error = @last_error,
    failure_kind = 'permanent'
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL;
