-- +goose Up
ALTER TABLE governance_manager_review_signals
    ADD COLUMN source_fingerprint text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX governance_manager_review_signals_source_fingerprint_uidx
    ON governance_manager_review_signals (source_fingerprint)
    WHERE source_fingerprint <> '';

-- +goose Down
DROP INDEX IF EXISTS governance_manager_review_signals_source_fingerprint_uidx;

ALTER TABLE governance_manager_review_signals
    DROP COLUMN IF EXISTS source_fingerprint;
