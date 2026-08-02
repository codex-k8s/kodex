-- name: inbox__insert_received :one
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
    last_error_code
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
    'RECEIVED',
    @max_attempts,
    @max_repairs,
    @error_code
)
RETURNING ordering_key::text;
