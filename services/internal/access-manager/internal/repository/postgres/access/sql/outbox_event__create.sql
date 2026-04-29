-- name: outbox_event__create :exec
INSERT INTO access_outbox_events (
    id, event_type, schema_version, aggregate_type, aggregate_id, payload, occurred_at, published_at
) VALUES (
    @id, @event_type, @schema_version, @aggregate_type, @aggregate_id, @payload, @occurred_at, @published_at
);
