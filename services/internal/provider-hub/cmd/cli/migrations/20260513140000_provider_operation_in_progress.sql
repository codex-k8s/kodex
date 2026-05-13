-- +goose Up
ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_status_chk,
    ADD CONSTRAINT provider_hub_operations_status_chk
        CHECK (status IN ('in_progress', 'succeeded', 'failed', 'retryable_failed', 'denied'));

-- +goose Down
ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_status_chk,
    ADD CONSTRAINT provider_hub_operations_status_chk
        CHECK (status IN ('succeeded', 'failed', 'retryable_failed', 'denied'));
