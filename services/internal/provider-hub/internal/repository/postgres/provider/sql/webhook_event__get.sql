-- name: webhook_event__get :one
SELECT
    id,
    provider_slug,
    delivery_id,
    event_name,
    repository_provider_id,
    received_at,
    processing_status,
    payload_json,
    payload_sha256,
    last_error,
    retain_until
FROM provider_hub_webhook_events
WHERE id = @id;
