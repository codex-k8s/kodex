-- +goose Up
CREATE TABLE runtime_manager_build_contexts (
    id uuid PRIMARY KEY,
    status text NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    provider text NOT NULL,
    provider_owner text NOT NULL,
    provider_name text NOT NULL,
    source_ref text NOT NULL,
    source_commit_sha text NOT NULL,
    affected_service_keys_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    build_plan_fingerprint text NOT NULL,
    context_fingerprint text NOT NULL,
    source_snapshot_ref text NOT NULL DEFAULT '',
    source_snapshot_digest text NOT NULL DEFAULT '',
    build_context_ref text NOT NULL DEFAULT '',
    build_context_digest text NOT NULL DEFAULT '',
    started_at timestamptz,
    finished_at timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    next_action text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT runtime_manager_build_contexts_status_chk
        CHECK (status IN ('pending', 'running', 'ready', 'failed')),
    CONSTRAINT runtime_manager_build_contexts_provider_chk
        CHECK (provider <> '' AND provider_owner <> '' AND provider_name <> ''),
    CONSTRAINT runtime_manager_build_contexts_source_chk
        CHECK (source_ref <> '' AND source_commit_sha <> ''),
    CONSTRAINT runtime_manager_build_contexts_service_keys_chk
        CHECK (jsonb_typeof(affected_service_keys_json) = 'array'),
    CONSTRAINT runtime_manager_build_contexts_fingerprint_chk
        CHECK (build_plan_fingerprint <> '' AND context_fingerprint <> ''),
    CONSTRAINT runtime_manager_build_contexts_source_snapshot_chk
        CHECK ((source_snapshot_ref = '' AND source_snapshot_digest = '') OR (source_snapshot_ref <> '' AND source_snapshot_digest <> '')),
    CONSTRAINT runtime_manager_build_contexts_ready_chk
        CHECK (status <> 'ready' OR (build_context_ref <> '' AND build_context_digest <> '')),
    CONSTRAINT runtime_manager_build_contexts_finished_chk
        CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    CONSTRAINT runtime_manager_build_contexts_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX runtime_manager_build_contexts_fingerprint_uidx
    ON runtime_manager_build_contexts (context_fingerprint);

CREATE INDEX runtime_manager_build_contexts_project_status_idx
    ON runtime_manager_build_contexts (project_id, status);

CREATE INDEX runtime_manager_build_contexts_repository_commit_idx
    ON runtime_manager_build_contexts (repository_id, source_commit_sha);

-- +goose Down
DROP TABLE runtime_manager_build_contexts;
