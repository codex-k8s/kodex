-- name: outbox_event__mark_published :exec
UPDATE provider_hub_outbox_events
SET
    published_at = @published_at,
    locked_until = NULL,
    last_error = '',
    failure_kind = ''
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL
  AND failed_permanently_at IS NULL;
