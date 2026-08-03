-- name: inbox__insert_stale :one
INSERT INTO runtime_inbox_events (
    consumer_name,
    consumer_scope,
    event_id,
    event_digest,
    event_name,
    event_version,
    schema_version,
    occurred_at,
    organization_id,
    aggregate_type,
    aggregate_id,
    aggregate_version,
    event_sequence,
    state,
    max_attempts,
    max_repairs,
    processed_at,
    cleanup_after
)
VALUES (
    @consumer_name,
    @consumer_scope,
    @event_id::uuid,
    @event_digest,
    @event_name,
    @event_version,
    @schema_version,
    @occurred_at,
    @organization_id,
    @aggregate_type,
    @aggregate_id,
    @aggregate_version,
    @event_sequence,
    'STALE',
    @max_attempts,
    @max_repairs,
    clock_timestamp(),
    clock_timestamp() + make_interval(secs => @retention_seconds)
)
RETURNING ordering_key::text;
