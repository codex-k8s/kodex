-- name: provider_event__insert :one
INSERT INTO provider_hub_provider_events (
    id,
    source_webhook_event_id,
    event_type,
    aggregate_type,
    aggregate_id,
    payload_json,
    occurred_at
) VALUES (
    @id,
    @source_webhook_event_id,
    @event_type,
    @aggregate_type,
    @aggregate_id,
    @payload_json::jsonb,
    @occurred_at
)
RETURNING
    id,
    source_webhook_event_id,
    event_type,
    aggregate_type,
    aggregate_id,
    payload_json,
    occurred_at;
