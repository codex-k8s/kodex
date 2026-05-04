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
ON CONFLICT (event_id) DO UPDATE
SET event_id = EXCLUDED.event_id
WHERE
    platform_event_log.source_service = EXCLUDED.source_service
    AND platform_event_log.event_type = EXCLUDED.event_type
    AND platform_event_log.schema_version = EXCLUDED.schema_version
    AND platform_event_log.aggregate_type = EXCLUDED.aggregate_type
    AND platform_event_log.aggregate_id = EXCLUDED.aggregate_id
    AND platform_event_log.payload = EXCLUDED.payload
    AND platform_event_log.occurred_at = EXCLUDED.occurred_at;
