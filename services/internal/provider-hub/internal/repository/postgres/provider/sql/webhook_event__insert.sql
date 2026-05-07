-- name: webhook_event__insert :one
INSERT INTO provider_hub_webhook_events (
    id,
    provider_slug,
    delivery_id,
    event_name,
    repository_provider_id,
    received_at,
    processing_status,
    payload_json,
    last_error,
    retain_until
) VALUES (
    @id,
    @provider_slug,
    @delivery_id,
    @event_name,
    @repository_provider_id,
    @received_at,
    @processing_status,
    @payload_json::jsonb,
    @last_error,
    @retain_until
)
ON CONFLICT (provider_slug, delivery_id) DO NOTHING
RETURNING
    id,
    provider_slug,
    delivery_id,
    event_name,
    repository_provider_id,
    received_at,
    processing_status,
    payload_json,
    last_error,
    retain_until;
