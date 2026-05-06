-- +goose Up
CREATE TABLE project_catalog_projects (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    slug text NOT NULL,
    display_name text NOT NULL,
    description text NOT NULL DEFAULT '',
    icon_object_uri text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (organization_id, slug),
    CONSTRAINT project_catalog_projects_status_chk
        CHECK (status IN ('active', 'archived', 'disabled')),
    CONSTRAINT project_catalog_projects_version_chk CHECK (version > 0)
);

CREATE INDEX project_catalog_projects_org_status_slug_idx
    ON project_catalog_projects (organization_id, status, slug);

CREATE TABLE project_catalog_repositories (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    provider text NOT NULL,
    provider_owner text NOT NULL,
    provider_name text NOT NULL,
    web_url text NOT NULL DEFAULT '',
    default_branch text NOT NULL DEFAULT '',
    status text NOT NULL,
    provider_repository_id text NOT NULL DEFAULT '',
    icon_object_uri text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (project_id, id),
    CONSTRAINT project_catalog_repositories_provider_chk
        CHECK (provider IN ('github', 'gitlab')),
    CONSTRAINT project_catalog_repositories_status_chk
        CHECK (status IN ('active', 'pending', 'blocked', 'archived')),
    CONSTRAINT project_catalog_repositories_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX project_catalog_repositories_provider_active_uidx
    ON project_catalog_repositories (provider, provider_owner, provider_name)
    WHERE status <> 'archived';

CREATE INDEX project_catalog_repositories_project_status_provider_idx
    ON project_catalog_repositories (project_id, status, provider, provider_owner, provider_name);

CREATE TABLE project_catalog_services_policies (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    source_repository_id uuid,
    source_path text NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    source_commit_sha text NOT NULL,
    source_blob_sha text NOT NULL DEFAULT '',
    policy_version bigint NOT NULL,
    content_hash text NOT NULL,
    validated_payload jsonb NOT NULL,
    validation_status text NOT NULL,
    projection_status text NOT NULL,
    imported_at timestamptz NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (project_id, policy_version),
    CONSTRAINT project_catalog_services_policies_source_repository_fk
        FOREIGN KEY (project_id, source_repository_id)
        REFERENCES project_catalog_repositories(project_id, id),
    CONSTRAINT project_catalog_services_policies_validation_status_chk
        CHECK (validation_status IN ('valid', 'invalid', 'stale')),
    CONSTRAINT project_catalog_services_policies_projection_status_chk
        CHECK (projection_status IN ('synced', 'pending', 'failed', 'overridden')),
    CONSTRAINT project_catalog_services_policies_version_chk CHECK (version > 0),
    CONSTRAINT project_catalog_services_policies_policy_version_chk CHECK (policy_version > 0)
);

CREATE INDEX project_catalog_services_policies_active_idx
    ON project_catalog_services_policies (project_id, projection_status, policy_version DESC);

CREATE INDEX project_catalog_services_policies_source_check_idx
    ON project_catalog_services_policies (source_repository_id, source_path, source_commit_sha, content_hash);

CREATE TABLE project_catalog_service_descriptors (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    services_policy_id uuid NOT NULL REFERENCES project_catalog_services_policies(id),
    repository_id uuid,
    service_key text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    kind text NOT NULL,
    root_path text NOT NULL DEFAULT '',
    documentation_scope_id text NOT NULL DEFAULT '',
    depends_on_service_keys text[] NOT NULL DEFAULT '{}',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (services_policy_id, service_key),
    CONSTRAINT project_catalog_service_descriptors_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES project_catalog_repositories(project_id, id),
    CONSTRAINT project_catalog_service_descriptors_kind_chk
        CHECK (kind IN ('backend', 'frontend', 'worker', 'documentation', 'package', 'other')),
    CONSTRAINT project_catalog_service_descriptors_status_chk
        CHECK (status IN ('active', 'disabled', 'stale')),
    CONSTRAINT project_catalog_service_descriptors_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX project_catalog_service_descriptors_active_uidx
    ON project_catalog_service_descriptors (project_id, service_key)
    WHERE status = 'active';

CREATE INDEX project_catalog_service_descriptors_project_status_key_idx
    ON project_catalog_service_descriptors (project_id, status, service_key);

CREATE INDEX project_catalog_service_descriptors_repository_status_key_idx
    ON project_catalog_service_descriptors (repository_id, status, service_key);

CREATE TABLE project_catalog_documentation_sources (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    repository_id uuid,
    scope_type text NOT NULL,
    scope_id text NOT NULL DEFAULT '',
    local_path text NOT NULL,
    access_mode text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT project_catalog_documentation_sources_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES project_catalog_repositories(project_id, id),
    CONSTRAINT project_catalog_documentation_sources_scope_type_chk
        CHECK (scope_type IN ('project', 'service', 'dependency', 'guidance_ref')),
    CONSTRAINT project_catalog_documentation_sources_access_mode_chk
        CHECK (access_mode IN ('read', 'write')),
    CONSTRAINT project_catalog_documentation_sources_status_chk
        CHECK (status IN ('active', 'disabled', 'blocked')),
    CONSTRAINT project_catalog_documentation_sources_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX project_catalog_documentation_sources_active_uidx
    ON project_catalog_documentation_sources (project_id, scope_type, scope_id, local_path)
    WHERE status = 'active';

CREATE INDEX project_catalog_documentation_sources_scope_status_idx
    ON project_catalog_documentation_sources (project_id, scope_type, scope_id, status);

CREATE INDEX project_catalog_documentation_sources_repository_status_idx
    ON project_catalog_documentation_sources (repository_id, status);

CREATE TABLE project_catalog_branch_rules (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    repository_id uuid,
    pattern text NOT NULL,
    required_checks text[] NOT NULL DEFAULT '{}',
    merge_policy text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT project_catalog_branch_rules_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES project_catalog_repositories(project_id, id),
    CONSTRAINT project_catalog_branch_rules_merge_policy_chk
        CHECK (merge_policy IN ('merge', 'squash', 'rebase', 'manual')),
    CONSTRAINT project_catalog_branch_rules_status_chk
        CHECK (status IN ('active', 'disabled')),
    CONSTRAINT project_catalog_branch_rules_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX project_catalog_branch_rules_active_uidx
    ON project_catalog_branch_rules (project_id, COALESCE(repository_id, '00000000-0000-0000-0000-000000000000'::uuid), pattern)
    WHERE status = 'active';

CREATE INDEX project_catalog_branch_rules_project_repository_status_idx
    ON project_catalog_branch_rules (project_id, repository_id, status);

CREATE TABLE project_catalog_release_policies (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    name text NOT NULL,
    branch_pattern text NOT NULL,
    rollout_strategy text NOT NULL,
    rollback_policy text NOT NULL,
    risk_profile_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (project_id, id),
    CONSTRAINT project_catalog_release_policies_rollout_strategy_chk
        CHECK (rollout_strategy IN ('direct', 'staged', 'canary')),
    CONSTRAINT project_catalog_release_policies_rollback_policy_chk
        CHECK (rollback_policy IN ('manual', 'automatic_on_gate', 'automatic_on_alert')),
    CONSTRAINT project_catalog_release_policies_status_chk
        CHECK (status IN ('active', 'disabled', 'archived')),
    CONSTRAINT project_catalog_release_policies_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX project_catalog_release_policies_active_uidx
    ON project_catalog_release_policies (project_id, name)
    WHERE status <> 'archived';

CREATE INDEX project_catalog_release_policies_project_status_idx
    ON project_catalog_release_policies (project_id, status);

CREATE TABLE project_catalog_release_lines (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    release_policy_id uuid NOT NULL,
    name text NOT NULL,
    branch_pattern text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (project_id, release_policy_id, name),
    CONSTRAINT project_catalog_release_lines_policy_fk
        FOREIGN KEY (project_id, release_policy_id)
        REFERENCES project_catalog_release_policies(project_id, id),
    CONSTRAINT project_catalog_release_lines_status_chk
        CHECK (status IN ('active', 'disabled', 'archived')),
    CONSTRAINT project_catalog_release_lines_version_chk CHECK (version > 0)
);

CREATE INDEX project_catalog_release_lines_project_policy_status_idx
    ON project_catalog_release_lines (project_id, release_policy_id, status);

CREATE TABLE project_catalog_placement_policies (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    repository_id uuid,
    service_key text NOT NULL DEFAULT '',
    allowed_cluster_refs text[] NOT NULL DEFAULT '{}',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT project_catalog_placement_policies_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES project_catalog_repositories(project_id, id),
    CONSTRAINT project_catalog_placement_policies_status_chk
        CHECK (status IN ('active', 'disabled')),
    CONSTRAINT project_catalog_placement_policies_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX project_catalog_placement_policies_active_uidx
    ON project_catalog_placement_policies (project_id, COALESCE(repository_id, '00000000-0000-0000-0000-000000000000'::uuid), service_key)
    WHERE status = 'active';

CREATE INDEX project_catalog_placement_policies_project_repository_status_idx
    ON project_catalog_placement_policies (project_id, repository_id, status);

CREATE TABLE project_catalog_policy_overrides (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    target_type text NOT NULL,
    target_id uuid,
    payload jsonb NOT NULL,
    reason text NOT NULL DEFAULT '',
    status text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_by_actor_ref text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT project_catalog_policy_overrides_target_type_chk
        CHECK (target_type IN (
            'services_policy',
            'branch_rules',
            'release_policy',
            'release_line',
            'placement_policy',
            'documentation_source'
        )),
    CONSTRAINT project_catalog_policy_overrides_status_chk
        CHECK (status IN ('active', 'expired', 'cancelled')),
    CONSTRAINT project_catalog_policy_overrides_version_chk CHECK (version > 0)
);

CREATE INDEX project_catalog_policy_overrides_project_target_status_expires_idx
    ON project_catalog_policy_overrides (project_id, target_type, status, expires_at);

CREATE TABLE project_catalog_policy_edit_proposals (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES project_catalog_projects(id),
    repository_id uuid NOT NULL,
    source_path text NOT NULL,
    requested_changes jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT project_catalog_policy_edit_proposals_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES project_catalog_repositories(project_id, id)
);

CREATE INDEX project_catalog_policy_edit_proposals_project_status_idx
    ON project_catalog_policy_edit_proposals (project_id, status, created_at);

CREATE TABLE project_catalog_command_results (
    key text PRIMARY KEY,
    command_id uuid,
    idempotency_key text NOT NULL DEFAULT '',
    operation text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    result_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT project_catalog_command_results_identity_chk
        CHECK (command_id IS NOT NULL OR idempotency_key <> '')
);

CREATE UNIQUE INDEX project_catalog_command_results_command_id_uidx
    ON project_catalog_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX project_catalog_command_results_idempotency_uidx
    ON project_catalog_command_results (operation, idempotency_key)
    WHERE idempotency_key <> '';

CREATE TABLE project_catalog_outbox_events (
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
    CONSTRAINT project_catalog_outbox_events_failure_kind_chk
        CHECK (failure_kind IN ('', 'transient', 'permanent'))
);

CREATE INDEX project_catalog_outbox_events_unpublished_idx
    ON project_catalog_outbox_events (occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX project_catalog_outbox_events_claim_idx
    ON project_catalog_outbox_events (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX project_catalog_outbox_events_lock_idx
    ON project_catalog_outbox_events (locked_until)
    WHERE published_at IS NULL AND locked_until IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS project_catalog_outbox_events;
DROP TABLE IF EXISTS project_catalog_command_results;
DROP TABLE IF EXISTS project_catalog_policy_edit_proposals;
DROP TABLE IF EXISTS project_catalog_policy_overrides;
DROP TABLE IF EXISTS project_catalog_placement_policies;
DROP TABLE IF EXISTS project_catalog_release_lines;
DROP TABLE IF EXISTS project_catalog_release_policies;
DROP TABLE IF EXISTS project_catalog_branch_rules;
DROP TABLE IF EXISTS project_catalog_documentation_sources;
DROP TABLE IF EXISTS project_catalog_service_descriptors;
DROP TABLE IF EXISTS project_catalog_services_policies;
DROP TABLE IF EXISTS project_catalog_repositories;
DROP TABLE IF EXISTS project_catalog_projects;
