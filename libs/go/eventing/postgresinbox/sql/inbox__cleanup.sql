-- name: inbox__cleanup :one
WITH candidates AS (
    SELECT consumer_name, consumer_scope, event_id
    FROM runtime_inbox_events
    WHERE state IN ('COMPLETED', 'STALE')
      AND cleanup_after <= clock_timestamp()
      AND processed_at <= clock_timestamp()
          - make_interval(secs => @retention_seconds)
    ORDER BY cleanup_after, consumer_name, consumer_scope, event_id
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
), deleted AS (
    DELETE FROM runtime_inbox_events AS event
    USING candidates AS candidate
    WHERE event.consumer_name = candidate.consumer_name
      AND event.consumer_scope = candidate.consumer_scope
      AND event.event_id = candidate.event_id
    RETURNING event.event_id
)
SELECT count(*)::integer
FROM deleted;
