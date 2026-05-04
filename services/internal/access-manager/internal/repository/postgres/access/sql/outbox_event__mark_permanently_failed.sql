-- name: outbox_event__mark_permanently_failed :exec
UPDATE access_outbox_events
SET
    locked_until = NULL,
    failed_permanently_at = @failed_permanently_at,
    failure_kind = 'permanent',
    last_error = @last_error
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL;
