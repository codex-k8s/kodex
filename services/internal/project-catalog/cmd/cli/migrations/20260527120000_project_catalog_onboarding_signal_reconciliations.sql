-- +goose Up
CREATE TABLE project_catalog_onboarding_signal_reconciliations (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    repository_id uuid NOT NULL,
    signal_kind text NOT NULL,
    signal_key text NOT NULL,
    signal_fingerprint text NOT NULL,
    provider_slug text NOT NULL,
    repository_full_name text NOT NULL,
    provider_repository_id text NOT NULL DEFAULT '',
    base_branch text NOT NULL DEFAULT '',
    source_ref text NOT NULL DEFAULT '',
    source_commit_sha text NOT NULL DEFAULT '',
    artifact_ref text NOT NULL DEFAULT '',
    artifact_digest text NOT NULL DEFAULT '',
    artifact_version text NOT NULL DEFAULT '',
    content_hash text NOT NULL DEFAULT '',
    status text NOT NULL,
    error_code text NOT NULL DEFAULT '',
    error_summary text NOT NULL DEFAULT '',
    summary text NOT NULL DEFAULT '',
    services_policy_id uuid REFERENCES project_catalog_services_policies(id),
    services_policy_version bigint NOT NULL DEFAULT 0,
    observed_at timestamptz NOT NULL,
    completed_at timestamptz,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (project_id, signal_kind, signal_key),
    CONSTRAINT project_catalog_onboarding_signal_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES project_catalog_repositories(project_id, id),
    CONSTRAINT project_catalog_onboarding_signal_kind_chk
        CHECK (signal_kind IN ('bootstrap_merge', 'adoption_scan')),
    CONSTRAINT project_catalog_onboarding_signal_status_chk
        CHECK (status IN ('processing', 'imported', 'failed', 'received', 'needs_review')),
    CONSTRAINT project_catalog_onboarding_signal_version_chk CHECK (version > 0),
    CONSTRAINT project_catalog_onboarding_signal_policy_version_chk CHECK (services_policy_version >= 0)
);

CREATE INDEX project_catalog_onboarding_signal_repository_status_idx
    ON project_catalog_onboarding_signal_reconciliations (project_id, repository_id, status, updated_at DESC);

CREATE INDEX project_catalog_onboarding_signal_policy_idx
    ON project_catalog_onboarding_signal_reconciliations (services_policy_id)
    WHERE services_policy_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS project_catalog_onboarding_signal_reconciliations;
