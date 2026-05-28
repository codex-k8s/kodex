-- name: webhook_event__cleanup_expired_payloads :many
WITH candidates AS (
    SELECT id
    FROM provider_hub_webhook_events
    WHERE processing_status IN ('pending', 'failed')
      AND retain_until <= @now
      AND (
        last_error <> 'payload_expired'
        OR (payload_json ->> 'payload_storage') IS DISTINCT FROM 'expired_after_retention'
      )
    ORDER BY retain_until, received_at, id
    LIMIT @limit::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE provider_hub_webhook_events AS event
SET
    processing_status = 'failed',
    payload_json = jsonb_strip_nulls(jsonb_build_object(
        'provider_slug', event.provider_slug,
        'delivery_id', event.delivery_id,
        'event_name', event.event_name,
        'repository_provider_id', NULLIF(event.repository_provider_id, ''),
        'payload_sha256', event.payload_sha256,
        'payload_storage', 'expired_after_retention',
        'payload_cleanup_reason', 'payload_expired',
        'payload_expired_at', to_char(@now::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
        'retain_until', to_char(event.retain_until AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )),
    last_error = 'payload_expired'
FROM candidates
WHERE event.id = candidates.id
RETURNING
    event.id,
    event.provider_slug,
    event.delivery_id,
    event.event_name,
    event.repository_provider_id,
    event.received_at,
    event.processing_status,
    event.payload_json,
    event.payload_sha256,
    event.last_error,
    event.retain_until;
