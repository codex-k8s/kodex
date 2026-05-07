-- +goose Up
CREATE UNIQUE INDEX provider_hub_limit_snapshots_identity_uidx
    ON provider_hub_limit_snapshots (external_account_id, provider_slug, limit_class, captured_at, source);

-- +goose Down
DROP INDEX IF EXISTS provider_hub_limit_snapshots_identity_uidx;
