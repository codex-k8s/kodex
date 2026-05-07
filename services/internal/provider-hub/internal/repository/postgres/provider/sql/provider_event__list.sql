-- name: provider_event__list :many
SELECT
    id,
    source_webhook_event_id,
    event_type,
    aggregate_type,
    aggregate_id,
    payload_json,
    occurred_at
FROM provider_hub_provider_events
WHERE (@source_webhook_event_id::uuid IS NULL OR source_webhook_event_id = @source_webhook_event_id)
ORDER BY occurred_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
