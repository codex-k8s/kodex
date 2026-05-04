-- name: outbox_event__mark_failed :exec
UPDATE access_outbox_events
SET
    locked_until = NULL,
    next_attempt_at = @next_attempt_at,
    failure_kind = 'transient',
    last_error = @last_error
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL;
