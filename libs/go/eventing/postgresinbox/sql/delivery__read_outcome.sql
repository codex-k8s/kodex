-- name: delivery__read_outcome :one
SELECT
    event.event_digest,
    event.state,
    event.event_sequence,
    cursor.last_sequence,
    event.attempts,
    event.max_attempts,
    event.available_at <= clock_timestamp() AS available_now,
    event.state = 'PROCESSING'
        AND event.lease_expires_at > clock_timestamp() AS lease_active
FROM runtime_inbox_events AS event
JOIN runtime_event_cursors AS cursor
  ON cursor.consumer_name = event.consumer_name
 AND cursor.consumer_scope = event.consumer_scope
 AND cursor.ordering_key = event.ordering_key
WHERE event.consumer_name = @consumer_name
  AND event.consumer_scope = @consumer_scope
  AND event.event_id = @event_id::uuid;
