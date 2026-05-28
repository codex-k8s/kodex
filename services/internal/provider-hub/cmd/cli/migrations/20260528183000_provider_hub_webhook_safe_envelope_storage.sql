-- +goose Up
UPDATE provider_hub_webhook_events
SET payload_json = jsonb_strip_nulls(jsonb_build_object(
    'provider_slug', provider_slug,
    'delivery_id', delivery_id,
    'event_name', event_name,
    'repository_provider_id', NULLIF(repository_provider_id, ''),
    'payload_sha256', payload_sha256,
    'payload_storage', 'safe_envelope_only',
    'payload_cleanup_reason', CASE
        WHEN processing_status IN ('pending', 'failed') THEN 'raw_payload_removed'
        ELSE NULL
    END,
    'retain_until', to_char(retain_until AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
))
WHERE COALESCE(payload_json ->> 'payload_storage', '') IN ('', 'retained_for_retry');

ALTER TABLE provider_hub_webhook_events
    ADD CONSTRAINT provider_hub_webhook_events_safe_payload_storage_chk
    CHECK (
        payload_json ? 'payload_storage'
        AND payload_json ->> 'payload_storage' IN (
            'safe_envelope_only',
            'retained_for_retry',
            'redacted_after_terminal_processing',
            'expired_after_retention'
        )
    );

-- +goose Down
ALTER TABLE provider_hub_webhook_events
    DROP CONSTRAINT IF EXISTS provider_hub_webhook_events_safe_payload_storage_chk;
