-- +goose Up
ALTER TABLE provider_hub_webhook_events
    ADD COLUMN payload_sha256 text NOT NULL DEFAULT '';

CREATE EXTENSION IF NOT EXISTS pgcrypto;

UPDATE provider_hub_webhook_events
SET payload_sha256 = encode(digest(convert_to(payload_json::text, 'UTF8'), 'sha256'), 'hex')
WHERE payload_sha256 = '';

UPDATE provider_hub_webhook_events
SET payload_json = jsonb_strip_nulls(jsonb_build_object(
    'provider_slug', provider_slug,
    'delivery_id', delivery_id,
    'event_name', event_name,
    'repository_provider_id', NULLIF(repository_provider_id, ''),
    'payload_sha256', payload_sha256,
    'payload_storage', 'redacted_after_terminal_processing',
    'retain_until', to_char(retain_until AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
))
WHERE processing_status IN ('processed', 'ignored');

ALTER TABLE provider_hub_webhook_events
    ADD CONSTRAINT provider_hub_webhook_events_payload_sha256_chk
    CHECK (payload_sha256 ~ '^[0-9a-f]{64}$');

-- +goose Down
ALTER TABLE provider_hub_webhook_events
    DROP CONSTRAINT IF EXISTS provider_hub_webhook_events_payload_sha256_chk;

ALTER TABLE provider_hub_webhook_events
    DROP COLUMN IF EXISTS payload_sha256;
