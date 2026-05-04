-- name: event_log__get_stored_event_by_id :one
SELECT
    sequence_id,
    event_id,
    source_service,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    payload,
    occurred_at,
    recorded_at
FROM platform_event_log
WHERE event_id = @event_id;
