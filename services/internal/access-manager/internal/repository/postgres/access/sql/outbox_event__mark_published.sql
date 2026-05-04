-- name: outbox_event__mark_published :exec
UPDATE access_outbox_events
SET
    published_at = @published_at,
    locked_until = NULL,
    last_error = ''
WHERE id = @id
  AND attempt_count = @attempt_count
  AND published_at IS NULL;
