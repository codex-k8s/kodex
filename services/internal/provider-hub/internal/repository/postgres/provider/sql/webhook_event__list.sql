-- name: webhook_event__list :many
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
WHERE (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (@delivery_id::text = '' OR delivery_id = @delivery_id)
  AND (cardinality(@event_names::text[]) = 0 OR event_name = ANY(@event_names::text[]))
  AND (cardinality(@processing_statuses::text[]) = 0 OR processing_status = ANY(@processing_statuses::text[]))
  AND (@repository_provider_id::text = '' OR repository_provider_id = @repository_provider_id)
  AND (@received_since::timestamptz IS NULL OR received_at >= @received_since)
  AND (@received_until::timestamptz IS NULL OR received_at <= @received_until)
ORDER BY received_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
