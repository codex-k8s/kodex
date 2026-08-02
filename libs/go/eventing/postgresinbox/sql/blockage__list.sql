-- name: blockage__list :many
WITH ranked AS (
    SELECT
        event.*,
        row_number() OVER (
            PARTITION BY event.ordering_key
            ORDER BY event.event_sequence
        ) AS predecessor_rank
    FROM runtime_inbox_events AS event
    WHERE event.consumer_name = @consumer_name
      AND event.consumer_scope = @consumer_scope
      AND event.state NOT IN ('COMPLETED', 'STALE')
), blockers AS (
    SELECT *
    FROM ranked
    WHERE predecessor_rank = 1
      AND (
          @after_received_at::timestamptz IS NULL
          OR (received_at, event_id) >
             (@after_received_at::timestamptz, @after_event_id::uuid)
      )
    ORDER BY received_at, event_id
    LIMIT @page_limit
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
FROM blockers AS blocker
LEFT JOIN runtime_event_cursors AS cursor
  ON cursor.consumer_name = blocker.consumer_name
 AND cursor.consumer_scope = blocker.consumer_scope
 AND cursor.ordering_key = blocker.ordering_key
ORDER BY blocker.received_at, blocker.event_id;
