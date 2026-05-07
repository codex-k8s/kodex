-- name: webhook_event__get_by_delivery :one
SELECT
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
FROM provider_hub_webhook_events
WHERE provider_slug = @provider_slug
  AND delivery_id = @delivery_id;
