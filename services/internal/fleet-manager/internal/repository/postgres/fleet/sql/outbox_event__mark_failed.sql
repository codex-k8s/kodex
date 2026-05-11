-- name: outbox_event__mark_failed :exec
UPDATE fleet_manager_outbox_events
SET
    next_attempt_at = @next_attempt_at::timestamptz,
    locked_until = NULL,
    failure_kind = 'transient',
    last_error = @last_error
WHERE id = @id::uuid
  AND attempt_count = @attempt_count::integer
  AND published_at IS NULL
  AND failed_permanently_at IS NULL;
