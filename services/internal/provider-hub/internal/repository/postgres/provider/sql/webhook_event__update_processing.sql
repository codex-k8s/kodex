-- name: webhook_event__update_processing :one
UPDATE provider_hub_webhook_events
SET
    processing_status = @processing_status,
    payload_json = @payload_json::jsonb,
    payload_sha256 = @payload_sha256,
    last_error = @last_error
WHERE id = @id
  AND processing_status IN ('pending', 'failed')
RETURNING
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
    retain_until;
