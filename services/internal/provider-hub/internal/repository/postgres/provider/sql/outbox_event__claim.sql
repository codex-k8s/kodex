-- name: outbox_event__claim :many
WITH candidates AS (
    SELECT id
    FROM provider_hub_outbox_events
    WHERE published_at IS NULL
      AND failed_permanently_at IS NULL
      AND next_attempt_at <= @now
      AND (locked_until IS NULL OR locked_until <= @now)
    ORDER BY occurred_at, id
    LIMIT @limit
    FOR UPDATE SKIP LOCKED
)
UPDATE provider_hub_outbox_events AS event
SET
    locked_until = @locked_until,
    attempt_count = event.attempt_count + 1
FROM candidates
WHERE event.id = candidates.id
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
    event.failed_permanently_at,
    event.failure_kind,
    event.last_error;
