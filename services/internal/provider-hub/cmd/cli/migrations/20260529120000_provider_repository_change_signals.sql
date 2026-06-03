-- +goose Up
CREATE TABLE provider_hub_repository_change_signals (
    id uuid PRIMARY KEY,
    signal_key text NOT NULL,
    kind text NOT NULL,
    provider_slug text NOT NULL,
    project_id uuid,
    repository_id uuid,
    repository_full_name text NOT NULL,
    provider_repository_id text NOT NULL,
    ref text NOT NULL,
    base_branch text NOT NULL,
    commit_sha text NOT NULL,
    before_sha text NOT NULL,
    source_ref text NOT NULL,
    pull_request_number bigint NOT NULL DEFAULT 0,
    pull_request_provider_id text NOT NULL DEFAULT '',
    pull_request_url text NOT NULL DEFAULT '',
    path_summary_status text NOT NULL,
    changed_path_count bigint NOT NULL DEFAULT 0,
    path_digest text NOT NULL,
    path_categories_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    services_policy_changed boolean NOT NULL DEFAULT false,
    deploy_relevant_changed boolean NOT NULL DEFAULT false,
    change_fingerprint text NOT NULL,
    observed_at timestamptz NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT provider_hub_repository_change_signals_signal_key_uniq UNIQUE (signal_key),
    CONSTRAINT provider_hub_repository_change_signals_kind_chk CHECK (kind IN ('push', 'pull_request_merged')),
    CONSTRAINT provider_hub_repository_change_signals_provider_slug_chk CHECK (provider_slug IN ('github', 'gitlab')),
    CONSTRAINT provider_hub_repository_change_signals_path_status_chk CHECK (path_summary_status IN ('ready', 'unavailable', 'truncated')),
    CONSTRAINT provider_hub_repository_change_signals_count_chk CHECK (changed_path_count >= 0),
    CONSTRAINT provider_hub_repository_change_signals_status_chk CHECK (status = 'observed'),
    CONSTRAINT provider_hub_repository_change_signals_version_chk CHECK (version > 0),
    CONSTRAINT provider_hub_repository_change_signals_non_empty_chk CHECK (
        btrim(signal_key) <> ''
        AND btrim(repository_full_name) <> ''
        AND btrim(provider_repository_id) <> ''
        AND btrim(ref) <> ''
        AND btrim(base_branch) <> ''
        AND btrim(commit_sha) <> ''
        AND btrim(path_digest) <> ''
        AND btrim(change_fingerprint) <> ''
    )
);

CREATE INDEX provider_hub_repository_change_signals_repository_idx
    ON provider_hub_repository_change_signals (provider_slug, repository_full_name, base_branch, observed_at DESC);

CREATE INDEX provider_hub_repository_change_signals_project_repository_idx
    ON provider_hub_repository_change_signals (project_id, repository_id, observed_at DESC)
    WHERE project_id IS NOT NULL AND repository_id IS NOT NULL;

CREATE INDEX provider_hub_repository_change_signals_deploy_relevant_idx
    ON provider_hub_repository_change_signals (provider_slug, repository_full_name, deploy_relevant_changed, observed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS provider_hub_repository_change_signals;
