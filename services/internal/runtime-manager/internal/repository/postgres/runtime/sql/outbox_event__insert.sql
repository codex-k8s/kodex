-- name: outbox_event__insert :exec
INSERT INTO runtime_manager_outbox_events (
    id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    payload,
    occurred_at,
    next_attempt_at
) VALUES (
    @id,
    @event_type,
    @schema_version,
    @aggregate_type,
    @aggregate_id,
    @payload::jsonb,
    @occurred_at,
    @next_attempt_at
);
