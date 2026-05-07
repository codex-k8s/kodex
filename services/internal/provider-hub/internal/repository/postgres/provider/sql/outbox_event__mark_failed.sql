-- name: outbox_event__mark_failed :exec
UPDATE provider_hub_outbox_events
SET
    next_attempt_at = @next_attempt_at,
    locked_until = NULL,
    failure_kind = 'transient',
    last_error = @last_error
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL
  AND failed_permanently_at IS NULL;
