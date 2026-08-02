-- name: blockage__get :one
WITH target AS (
    SELECT ordering_key, event_sequence
    FROM runtime_inbox_events
    WHERE consumer_name = @consumer_name
      AND consumer_scope = @consumer_scope
      AND event_id = @event_id::uuid
), blocker AS (
    SELECT event.*
    FROM runtime_inbox_events AS event
    JOIN target
      ON target.ordering_key = event.ordering_key
     AND event.event_sequence <= target.event_sequence
    WHERE event.consumer_name = @consumer_name
      AND event.consumer_scope = @consumer_scope
      AND event.state NOT IN ('COMPLETED', 'STALE')
    ORDER BY event.event_sequence
    LIMIT 1
)
SELECT
    blocker.event_id::text,
    blocker.event_digest,
    blocker.ordering_key::text,
    blocker.event_sequence,
    COALESCE(cursor.last_sequence, 0),
    blocker.state,
    blocker.attempts,
    blocker.max_attempts,
    blocker.repair_count,
    blocker.max_repairs,
    blocker.lease_generation,
    blocker.lease_fence,
    blocker.available_at,
    blocker.lease_expires_at,
    blocker.terminal_at,
    COALESCE(blocker.last_error_code, ''),
    blocker.received_at,
    blocker.available_at <= clock_timestamp(),
    blocker.state = 'PROCESSING'
        AND blocker.lease_expires_at > clock_timestamp()
FROM blocker
LEFT JOIN runtime_event_cursors AS cursor
  ON cursor.consumer_name = blocker.consumer_name
 AND cursor.consumer_scope = blocker.consumer_scope
 AND cursor.ordering_key = blocker.ordering_key;
