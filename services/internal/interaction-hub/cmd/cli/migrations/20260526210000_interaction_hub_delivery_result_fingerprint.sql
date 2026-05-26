-- +goose Up
ALTER TABLE interaction_hub_delivery_attempts
    ADD COLUMN result_fingerprint text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE interaction_hub_delivery_attempts
    DROP COLUMN IF EXISTS result_fingerprint;
