-- +goose Up
CREATE TABLE provider_hub_account_runtime_states (
    id uuid PRIMARY KEY,
    external_account_id uuid NOT NULL,
    provider_slug text NOT NULL,
    status text NOT NULL,
    last_checked_at timestamptz,
    last_success_at timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (external_account_id, provider_slug),
    CONSTRAINT provider_hub_account_runtime_states_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_account_runtime_states_status_chk
        CHECK (status IN ('active', 'reauthorization_required', 'limited', 'disabled', 'error')),
    CONSTRAINT provider_hub_account_runtime_states_version_chk CHECK (version > 0)
);

CREATE INDEX provider_hub_account_runtime_states_status_idx
    ON provider_hub_account_runtime_states (provider_slug, status, last_checked_at);

CREATE TABLE provider_hub_webhook_events (
    id uuid PRIMARY KEY,
    provider_slug text NOT NULL,
    delivery_id text NOT NULL,
    event_name text NOT NULL,
    repository_provider_id text NOT NULL DEFAULT '',
    received_at timestamptz NOT NULL,
    processing_status text NOT NULL,
    payload_json jsonb NOT NULL,
    last_error text NOT NULL DEFAULT '',
    retain_until timestamptz NOT NULL,
    UNIQUE (provider_slug, delivery_id),
    CONSTRAINT provider_hub_webhook_events_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_webhook_events_delivery_chk CHECK (delivery_id <> ''),
    CONSTRAINT provider_hub_webhook_events_event_name_chk CHECK (event_name <> ''),
    CONSTRAINT provider_hub_webhook_events_processing_status_chk
        CHECK (processing_status IN ('pending', 'processed', 'failed', 'ignored')),
    CONSTRAINT provider_hub_webhook_events_payload_chk CHECK (jsonb_typeof(payload_json) = 'object'),
    CONSTRAINT provider_hub_webhook_events_retention_chk CHECK (retain_until > received_at)
);

CREATE INDEX provider_hub_webhook_events_status_received_idx
    ON provider_hub_webhook_events (processing_status, received_at);

CREATE INDEX provider_hub_webhook_events_repository_idx
    ON provider_hub_webhook_events (provider_slug, repository_provider_id, received_at);

CREATE INDEX provider_hub_webhook_events_event_name_idx
    ON provider_hub_webhook_events (provider_slug, event_name, received_at);

CREATE INDEX provider_hub_webhook_events_retention_idx
    ON provider_hub_webhook_events (retain_until);

CREATE TABLE provider_hub_provider_events (
    id uuid PRIMARY KEY,
    source_webhook_event_id uuid REFERENCES provider_hub_webhook_events(id),
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload_json jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    CONSTRAINT provider_hub_provider_events_event_type_chk CHECK (event_type <> ''),
    CONSTRAINT provider_hub_provider_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT provider_hub_provider_events_aggregate_id_chk CHECK (aggregate_id <> ''),
    CONSTRAINT provider_hub_provider_events_payload_chk CHECK (jsonb_typeof(payload_json) = 'object')
);

CREATE INDEX provider_hub_provider_events_type_occurred_idx
    ON provider_hub_provider_events (event_type, occurred_at);

CREATE INDEX provider_hub_provider_events_aggregate_idx
    ON provider_hub_provider_events (aggregate_type, aggregate_id, occurred_at);

CREATE TABLE provider_hub_work_item_projections (
    id uuid PRIMARY KEY,
    provider_slug text NOT NULL,
    provider_work_item_id text NOT NULL,
    project_id uuid,
    repository_id uuid,
    repository_full_name text NOT NULL,
    kind text NOT NULL,
    number bigint NOT NULL,
    url text NOT NULL DEFAULT '',
    title text NOT NULL,
    state text NOT NULL,
    work_item_type text NOT NULL DEFAULT '',
    labels_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    assignees_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    milestone text NOT NULL DEFAULT '',
    project_fields_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    watermark_status text NOT NULL,
    watermark_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    body_digest text NOT NULL DEFAULT '',
    provider_updated_at timestamptz,
    synced_at timestamptz NOT NULL,
    drift_status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (provider_slug, provider_work_item_id),
    CONSTRAINT provider_hub_work_item_projections_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_work_item_projections_provider_id_chk CHECK (provider_work_item_id <> ''),
    CONSTRAINT provider_hub_work_item_projections_repository_chk CHECK (repository_full_name <> ''),
    CONSTRAINT provider_hub_work_item_projections_kind_chk CHECK (kind IN ('issue', 'pull_request', 'merge_request')),
    CONSTRAINT provider_hub_work_item_projections_number_chk CHECK (number > 0),
    CONSTRAINT provider_hub_work_item_projections_title_chk CHECK (title <> ''),
    CONSTRAINT provider_hub_work_item_projections_state_chk CHECK (state <> ''),
    CONSTRAINT provider_hub_work_item_projections_labels_chk CHECK (jsonb_typeof(labels_json) = 'array'),
    CONSTRAINT provider_hub_work_item_projections_assignees_chk CHECK (jsonb_typeof(assignees_json) = 'array'),
    CONSTRAINT provider_hub_work_item_projections_project_fields_chk CHECK (jsonb_typeof(project_fields_json) = 'object'),
    CONSTRAINT provider_hub_work_item_projections_watermark_status_chk
        CHECK (watermark_status IN ('missing', 'valid', 'invalid', 'stale')),
    CONSTRAINT provider_hub_work_item_projections_watermark_chk CHECK (jsonb_typeof(watermark_json) = 'object'),
    CONSTRAINT provider_hub_work_item_projections_drift_status_chk
        CHECK (drift_status IN ('fresh', 'suspected', 'stale', 'failed')),
    CONSTRAINT provider_hub_work_item_projections_version_chk CHECK (version > 0)
);

CREATE INDEX provider_hub_work_item_provider_ref_idx
    ON provider_hub_work_item_projections (provider_slug, repository_full_name, kind, number);

CREATE INDEX provider_hub_work_item_project_state_idx
    ON provider_hub_work_item_projections (project_id, kind, state, provider_updated_at);

CREATE INDEX provider_hub_work_item_drift_idx
    ON provider_hub_work_item_projections (drift_status, synced_at);

CREATE TABLE provider_hub_comment_projections (
    id uuid PRIMARY KEY,
    work_item_projection_id uuid NOT NULL REFERENCES provider_hub_work_item_projections(id) ON DELETE CASCADE,
    provider_comment_id text NOT NULL,
    kind text NOT NULL,
    author_provider_login text NOT NULL DEFAULT '',
    body_digest text NOT NULL DEFAULT '',
    summary text NOT NULL DEFAULT '',
    provider_created_at timestamptz,
    provider_updated_at timestamptz,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (work_item_projection_id, provider_comment_id),
    CONSTRAINT provider_hub_comment_projections_provider_id_chk CHECK (provider_comment_id <> ''),
    CONSTRAINT provider_hub_comment_projections_kind_chk CHECK (kind IN ('comment', 'review', 'mention', 'system')),
    CONSTRAINT provider_hub_comment_projections_version_chk CHECK (version > 0)
);

CREATE INDEX provider_hub_comment_projections_work_item_kind_idx
    ON provider_hub_comment_projections (work_item_projection_id, kind, provider_updated_at);

CREATE TABLE provider_hub_relationships (
    id uuid PRIMARY KEY,
    source_work_item_id uuid NOT NULL REFERENCES provider_hub_work_item_projections(id) ON DELETE CASCADE,
    target_work_item_id uuid REFERENCES provider_hub_work_item_projections(id) ON DELETE CASCADE,
    target_provider_ref text NOT NULL DEFAULT '',
    relationship_type text NOT NULL,
    source text NOT NULL,
    confidence text NOT NULL DEFAULT 'confirmed',
    created_at timestamptz NOT NULL,
    CONSTRAINT provider_hub_relationships_target_chk CHECK (target_work_item_id IS NOT NULL OR target_provider_ref <> ''),
    CONSTRAINT provider_hub_relationships_type_chk CHECK (relationship_type <> ''),
    CONSTRAINT provider_hub_relationships_source_chk
        CHECK (source IN ('provider', 'watermark', 'comment', 'manual', 'reconciliation')),
    CONSTRAINT provider_hub_relationships_confidence_chk
        CHECK (confidence IN ('confirmed', 'inferred', 'suspected'))
);

CREATE UNIQUE INDEX provider_hub_relationships_identity_uidx
    ON provider_hub_relationships (
        source_work_item_id,
        COALESCE(target_work_item_id, '00000000-0000-0000-0000-000000000000'::uuid),
        target_provider_ref,
        relationship_type
    );

CREATE INDEX provider_hub_relationships_target_idx
    ON provider_hub_relationships (target_work_item_id, relationship_type);

CREATE TABLE provider_hub_sync_cursors (
    id uuid PRIMARY KEY,
    provider_slug text NOT NULL,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    artifact_kind text NOT NULL,
    cursor_value text NOT NULL DEFAULT '',
    overlap_since timestamptz,
    priority text NOT NULL,
    last_success_at timestamptz,
    last_checked_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    rate_budget_state_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (provider_slug, scope_type, scope_ref, artifact_kind),
    CONSTRAINT provider_hub_sync_cursors_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_sync_cursors_scope_type_chk
        CHECK (scope_type IN ('repository', 'organization', 'work_item', 'package_source')),
    CONSTRAINT provider_hub_sync_cursors_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT provider_hub_sync_cursors_artifact_kind_chk
        CHECK (artifact_kind IN ('issue', 'pull_request', 'merge_request', 'comment', 'relationship', 'repository')),
    CONSTRAINT provider_hub_sync_cursors_priority_chk CHECK (priority IN ('hot', 'warm', 'cold')),
    CONSTRAINT provider_hub_sync_cursors_budget_chk CHECK (jsonb_typeof(rate_budget_state_json) = 'object'),
    CONSTRAINT provider_hub_sync_cursors_lease_chk
        CHECK ((lease_owner = '' AND lease_until IS NULL) OR (lease_owner <> '' AND lease_until IS NOT NULL)),
    CONSTRAINT provider_hub_sync_cursors_version_chk CHECK (version > 0)
);

CREATE INDEX provider_hub_sync_cursors_priority_idx
    ON provider_hub_sync_cursors (priority, last_checked_at);

CREATE INDEX provider_hub_sync_cursors_lease_idx
    ON provider_hub_sync_cursors (lease_until)
    WHERE lease_until IS NOT NULL;

CREATE TABLE provider_hub_limit_snapshots (
    id uuid PRIMARY KEY,
    external_account_id uuid NOT NULL,
    provider_slug text NOT NULL,
    limit_class text NOT NULL,
    remaining bigint,
    limit_value bigint,
    reset_at timestamptz,
    captured_at timestamptz NOT NULL,
    source text NOT NULL,
    CONSTRAINT provider_hub_limit_snapshots_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_limit_snapshots_limit_class_chk CHECK (limit_class <> ''),
    CONSTRAINT provider_hub_limit_snapshots_remaining_chk CHECK (remaining IS NULL OR remaining >= 0),
    CONSTRAINT provider_hub_limit_snapshots_limit_value_chk CHECK (limit_value IS NULL OR limit_value >= 0),
    CONSTRAINT provider_hub_limit_snapshots_source_chk CHECK (source <> '')
);

CREATE INDEX provider_hub_limit_snapshots_account_class_idx
    ON provider_hub_limit_snapshots (external_account_id, limit_class, captured_at);

CREATE TABLE provider_hub_operations (
    id uuid PRIMARY KEY,
    command_id text NOT NULL DEFAULT '',
    actor_id uuid,
    external_account_id uuid NOT NULL,
    provider_slug text NOT NULL,
    operation_type text NOT NULL,
    target_ref text NOT NULL,
    status text NOT NULL,
    result_ref text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    rate_limit_snapshot_id uuid REFERENCES provider_hub_limit_snapshots(id),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT provider_hub_operations_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_operations_operation_type_chk
        CHECK (operation_type IN ('create_issue', 'update_issue', 'create_comment', 'update_comment', 'create_pull_request', 'create_review_signal', 'update_relationship')),
    CONSTRAINT provider_hub_operations_target_ref_chk CHECK (target_ref <> ''),
    CONSTRAINT provider_hub_operations_status_chk CHECK (status IN ('succeeded', 'failed', 'retryable_failed', 'denied')),
    CONSTRAINT provider_hub_operations_finished_chk
        CHECK (finished_at IS NULL OR finished_at >= started_at),
    CONSTRAINT provider_hub_operations_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX provider_hub_operations_command_uidx
    ON provider_hub_operations (operation_type, command_id)
    WHERE command_id <> '';

CREATE INDEX provider_hub_operations_target_idx
    ON provider_hub_operations (provider_slug, target_ref, started_at);

CREATE INDEX provider_hub_operations_account_status_idx
    ON provider_hub_operations (external_account_id, status, started_at);

CREATE TABLE provider_hub_outbox_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00'::timestamptz,
    locked_until timestamptz,
    failed_permanently_at timestamptz,
    failure_kind text NOT NULL DEFAULT '',
    last_error text NOT NULL DEFAULT '',
    CONSTRAINT provider_hub_outbox_events_event_type_chk CHECK (event_type <> ''),
    CONSTRAINT provider_hub_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT provider_hub_outbox_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT provider_hub_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT provider_hub_outbox_events_attempt_count_chk CHECK (attempt_count >= 0),
    CONSTRAINT provider_hub_outbox_events_failure_kind_chk
        CHECK (failure_kind IN ('', 'transient', 'permanent'))
);

CREATE INDEX provider_hub_outbox_events_unpublished_idx
    ON provider_hub_outbox_events (occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX provider_hub_outbox_events_claim_idx
    ON provider_hub_outbox_events (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX provider_hub_outbox_events_lock_idx
    ON provider_hub_outbox_events (locked_until)
    WHERE published_at IS NULL AND locked_until IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS provider_hub_outbox_events;
DROP TABLE IF EXISTS provider_hub_operations;
DROP TABLE IF EXISTS provider_hub_limit_snapshots;
DROP TABLE IF EXISTS provider_hub_sync_cursors;
DROP TABLE IF EXISTS provider_hub_relationships;
DROP TABLE IF EXISTS provider_hub_comment_projections;
DROP TABLE IF EXISTS provider_hub_work_item_projections;
DROP TABLE IF EXISTS provider_hub_provider_events;
DROP TABLE IF EXISTS provider_hub_webhook_events;
DROP TABLE IF EXISTS provider_hub_account_runtime_states;
