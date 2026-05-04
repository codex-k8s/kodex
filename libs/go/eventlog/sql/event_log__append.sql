-- name: event_log__append :exec
INSERT INTO platform_event_log (
    event_id,
    source_service,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    payload,
    occurred_at,
    recorded_at
)
VALUES (
    @event_id,
    @source_service,
    @event_type,
    @schema_version,
    @aggregate_type,
    @aggregate_id,
    @payload,
    @occurred_at,
    @recorded_at
)
ON CONFLICT (event_id) DO NOTHING;
