-- name: outbox_event__claim :many
WITH selected AS (
    SELECT id
    FROM access_outbox_events
    WHERE published_at IS NULL
      AND next_attempt_at <= @now
      AND (locked_until IS NULL OR locked_until <= @now)
    ORDER BY occurred_at, id
    LIMIT @limit
    FOR UPDATE SKIP LOCKED
)
UPDATE access_outbox_events AS event
SET
    attempt_count = event.attempt_count + 1,
    locked_until = @locked_until,
    last_error = ''
FROM selected
WHERE event.id = selected.id
RETURNING
    event.id,
    event.event_type,
    event.schema_version,
    event.aggregate_type,
    event.aggregate_id,
    event.payload,
    event.occurred_at,
    event.published_at,
    event.attempt_count,
    event.next_attempt_at,
    event.locked_until,
    event.last_error;
