-- +goose Up
CREATE TABLE provider_hub_repository_merge_signals (
    id uuid PRIMARY KEY,
    signal_key text NOT NULL,
    kind text NOT NULL,
    provider_slug text NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    repository_full_name text NOT NULL,
    provider_repository_id text NOT NULL,
    work_item_projection_id uuid NOT NULL REFERENCES provider_hub_work_item_projections(id) ON DELETE RESTRICT,
    provider_work_item_id text NOT NULL,
    pull_request_number bigint NOT NULL,
    pull_request_provider_id text NOT NULL,
    pull_request_url text NOT NULL,
    base_branch text NOT NULL,
    head_branch text NOT NULL,
    merge_commit_sha text NOT NULL,
    source_ref text NOT NULL,
    related_provider_operation_ref text NOT NULL,
    watermark_digest text NOT NULL,
    observed_at timestamptz NOT NULL,
    merged_at timestamptz NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT provider_hub_repository_merge_signals_signal_key_uniq UNIQUE (signal_key),
    CONSTRAINT provider_hub_repository_merge_signals_kind_chk CHECK (kind IN ('bootstrap', 'adoption')),
    CONSTRAINT provider_hub_repository_merge_signals_provider_slug_chk CHECK (provider_slug IN ('github', 'gitlab')),
    CONSTRAINT provider_hub_repository_merge_signals_pull_request_number_chk CHECK (pull_request_number > 0),
    CONSTRAINT provider_hub_repository_merge_signals_status_chk CHECK (status = 'merged'),
    CONSTRAINT provider_hub_repository_merge_signals_version_chk CHECK (version > 0),
    CONSTRAINT provider_hub_repository_merge_signals_non_empty_chk CHECK (
        btrim(signal_key) <> ''
        AND btrim(repository_full_name) <> ''
        AND btrim(provider_repository_id) <> ''
        AND btrim(provider_work_item_id) <> ''
        AND btrim(pull_request_provider_id) <> ''
        AND btrim(pull_request_url) <> ''
        AND btrim(base_branch) <> ''
        AND btrim(head_branch) <> ''
        AND btrim(merge_commit_sha) <> ''
        AND btrim(source_ref) <> ''
        AND btrim(related_provider_operation_ref) <> ''
        AND btrim(watermark_digest) <> ''
    )
);

CREATE INDEX provider_hub_repository_merge_signals_repository_idx
    ON provider_hub_repository_merge_signals (provider_slug, repository_full_name, kind, merged_at DESC);

CREATE INDEX provider_hub_repository_merge_signals_project_repository_idx
    ON provider_hub_repository_merge_signals (project_id, repository_id, kind, merged_at DESC);

-- +goose Down
DROP TABLE IF EXISTS provider_hub_repository_merge_signals;
