-- name: outbox_event__mark_permanently_failed :exec
UPDATE fleet_manager_outbox_events
SET
    failed_permanently_at = @failed_permanently_at::timestamptz,
    locked_until = NULL,
    failure_kind = 'permanent',
    last_error = @last_error
WHERE id = @id::uuid
  AND attempt_count = @attempt_count::integer
  AND published_at IS NULL
  AND failed_permanently_at IS NULL;
