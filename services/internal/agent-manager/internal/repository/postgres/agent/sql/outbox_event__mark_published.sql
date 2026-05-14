-- name: outbox_event__mark_published :exec
UPDATE agent_manager_outbox_events
SET published_at = @published_at,
    locked_until = NULL,
    failure_kind = '',
    last_error = ''
WHERE id = @id
  AND attempt_count = @attempt_count;
