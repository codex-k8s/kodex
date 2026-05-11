-- name: outbox_event__mark_published :exec
UPDATE fleet_manager_outbox_events
SET
    published_at = @published_at::timestamptz,
    locked_until = NULL,
    failure_kind = '',
    last_error = ''
WHERE id = @id::uuid
  AND attempt_count = @attempt_count::integer
  AND published_at IS NULL
  AND failed_permanently_at IS NULL;
