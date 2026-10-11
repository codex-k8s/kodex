-- +goose Up
-- Проекция optional финансового наблюдения привязана к current credential.
ALTER TABLE control_plane.provider_accounts
    ADD COLUMN financial_observation jsonb,
    ADD COLUMN financial_received_at timestamptz,
    ADD COLUMN financial_credential_revision_id uuid,
    ADD COLUMN financial_collector_lease_id uuid,
    ADD COLUMN financial_collector_created_at timestamptz,
    ADD COLUMN financial_sequence bigint;

-- +goose Down
-- Forward-only: проекция сохраняется при откате приложения.
SELECT 1;
