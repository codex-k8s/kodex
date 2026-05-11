-- name: outbox_event__insert :exec
INSERT INTO fleet_manager_outbox_events (
    id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    payload,
    occurred_at,
    published_at
) VALUES (
    @id::uuid,
    @event_type,
    @schema_version::integer,
    @aggregate_type,
    @aggregate_id::uuid,
    @payload::jsonb,
    @occurred_at::timestamptz,
    @published_at::timestamptz
);
