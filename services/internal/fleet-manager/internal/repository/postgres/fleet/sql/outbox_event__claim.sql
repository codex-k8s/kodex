-- name: outbox_event__claim :many
UPDATE fleet_manager_outbox_events
SET
    locked_until = @locked_until::timestamptz,
    attempt_count = attempt_count + 1
WHERE id IN (
    SELECT id
    FROM fleet_manager_outbox_events
    WHERE published_at IS NULL
      AND failed_permanently_at IS NULL
      AND next_attempt_at <= @now::timestamptz
      AND (locked_until IS NULL OR locked_until <= @now::timestamptz)
    ORDER BY occurred_at
    LIMIT @limit::integer
    FOR UPDATE SKIP LOCKED
)
RETURNING
    id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    payload,
    occurred_at,
    published_at,
    attempt_count,
    next_attempt_at,
    locked_until,
    failed_permanently_at,
    failure_kind,
    last_error;
