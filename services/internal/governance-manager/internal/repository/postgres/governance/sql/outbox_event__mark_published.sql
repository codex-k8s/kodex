-- name: outbox_event__mark_published :exec
UPDATE governance_manager_outbox_events
SET
    published_at = @published_at,
    locked_until = NULL,
    last_error = '',
    failure_kind = ''
WHERE id = @id
  AND attempt_count = @attempt_count;
