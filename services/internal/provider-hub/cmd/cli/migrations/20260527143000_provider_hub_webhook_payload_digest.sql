-- +goose Up
ALTER TABLE provider_hub_webhook_events
    ADD COLUMN payload_sha256 text NOT NULL DEFAULT '';

ALTER TABLE provider_hub_webhook_events
    ADD CONSTRAINT provider_hub_webhook_events_payload_sha256_chk
    CHECK (payload_sha256 = '' OR payload_sha256 ~ '^[0-9a-f]{64}$');

-- +goose Down
ALTER TABLE provider_hub_webhook_events
    DROP CONSTRAINT IF EXISTS provider_hub_webhook_events_payload_sha256_chk;

ALTER TABLE provider_hub_webhook_events
    DROP COLUMN IF EXISTS payload_sha256;
