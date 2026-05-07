-- +goose Up
CREATE TABLE package_hub_outbox_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00'::timestamptz,
    locked_until timestamptz,
    failed_permanently_at timestamptz,
    failure_kind text NOT NULL DEFAULT '',
    last_error text NOT NULL DEFAULT '',
    CONSTRAINT package_hub_outbox_events_type_chk CHECK (event_type <> ''),
    CONSTRAINT package_hub_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT package_hub_outbox_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT package_hub_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT package_hub_outbox_events_attempt_count_chk CHECK (attempt_count >= 0),
    CONSTRAINT package_hub_outbox_events_failure_kind_chk CHECK (failure_kind IN ('', 'transient', 'permanent'))
);

CREATE INDEX package_hub_outbox_events_ready_idx
    ON package_hub_outbox_events (next_attempt_at, occurred_at, id)
    WHERE published_at IS NULL AND failed_permanently_at IS NULL;

CREATE INDEX package_hub_outbox_events_aggregate_idx
    ON package_hub_outbox_events (aggregate_type, aggregate_id, occurred_at);

-- +goose Down
DROP TABLE IF EXISTS package_hub_outbox_events;
