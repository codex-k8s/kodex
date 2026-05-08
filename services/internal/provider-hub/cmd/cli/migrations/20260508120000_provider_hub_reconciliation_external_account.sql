-- +goose Up
ALTER TABLE provider_hub_sync_cursors
    ADD COLUMN external_account_id uuid NOT NULL;

CREATE INDEX provider_hub_sync_cursors_account_priority_idx
    ON provider_hub_sync_cursors (external_account_id, priority, last_checked_at);

ALTER TABLE provider_hub_reconciliation_requests
    ADD COLUMN external_account_id uuid NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS provider_hub_sync_cursors_account_priority_idx;

ALTER TABLE provider_hub_reconciliation_requests
    DROP COLUMN IF EXISTS external_account_id;

ALTER TABLE provider_hub_sync_cursors
    DROP COLUMN IF EXISTS external_account_id;
