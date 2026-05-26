-- name: outbox_event__mark_failed :exec
UPDATE governance_manager_outbox_events
SET
    next_attempt_at = @next_attempt_at,
    locked_until = NULL,
    last_error = @last_error,
    failure_kind = 'retryable'
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL;
