-- +goose Up
ALTER TABLE access_outbox_events
    ADD COLUMN attempt_count integer NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00'::timestamptz,
    ADD COLUMN locked_until timestamptz,
    ADD COLUMN failed_permanently_at timestamptz,
    ADD COLUMN failure_kind text NOT NULL DEFAULT '',
    ADD COLUMN last_error text NOT NULL DEFAULT '',
    ADD CONSTRAINT access_outbox_events_failure_kind_chk
        CHECK (failure_kind IN ('', 'transient', 'permanent'));

CREATE INDEX access_outbox_events_claim_idx
    ON access_outbox_events (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX access_outbox_events_lock_idx
    ON access_outbox_events (locked_until)
    WHERE published_at IS NULL AND locked_until IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS access_outbox_events_lock_idx;
DROP INDEX IF EXISTS access_outbox_events_claim_idx;

ALTER TABLE access_outbox_events
    DROP CONSTRAINT IF EXISTS access_outbox_events_failure_kind_chk,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS failure_kind,
    DROP COLUMN IF EXISTS failed_permanently_at,
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS attempt_count;
