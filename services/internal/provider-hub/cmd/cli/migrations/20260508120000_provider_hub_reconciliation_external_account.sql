-- +goose Up
-- PRV-6.1 queue rows do not contain the selected external account yet.
-- They are ephemeral scheduler state and can be recreated by EnqueueReconciliation,
-- so the safe upgrade path for test clusters is to drop stale queue state before
-- making external_account_id required.
TRUNCATE TABLE provider_hub_sync_cursors, provider_hub_reconciliation_requests;

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
