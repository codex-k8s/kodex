-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS control_plane;

CREATE TABLE control_plane.installation (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version integer NOT NULL CHECK (schema_version = 1),
    platform_sequence bigint NOT NULL DEFAULT 0 CHECK (platform_sequence >= 0),
    bootstrapped_at timestamptz,
    onboarding_completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE control_plane.organizations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    authority_tenant_ref text UNIQUE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE control_plane.subjects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    issuer text NOT NULL CHECK (char_length(issuer) BETWEEN 1 AND 500),
    external_subject_digest text NOT NULL CHECK (external_subject_digest ~ '^[a-f0-9]{64}$'),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 160),
    email_masked text NOT NULL DEFAULT '',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, issuer, external_subject_digest)
);

CREATE TABLE control_plane.owner_claim_contracts (
    organization_id uuid PRIMARY KEY REFERENCES control_plane.organizations(id),
    stable_key text NOT NULL UNIQUE CHECK (stable_key = 'installation-owner'),
    state text NOT NULL CHECK (state IN ('PENDING_CLAIM', 'CLAIMED')),
    subject_id uuid REFERENCES control_plane.subjects(id),
    claimed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK ((state = 'PENDING_CLAIM' AND subject_id IS NULL AND claimed_at IS NULL) OR
           (state = 'CLAIMED' AND subject_id IS NOT NULL AND claimed_at IS NOT NULL))
);

CREATE TABLE control_plane.worker_grant_high_watermarks (
    workload_id text PRIMARY KEY CHECK (workload_id IN (
        'automation-scheduler', 'integration-gateway', 'runtime-controller',
        'role-image-builder', 'image-admission', 'image-promotion'
    )),
    revision bigint NOT NULL CHECK (revision > 0),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (expires_at > issued_at)
);

CREATE TABLE control_plane.memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid,
    subject_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    role text NOT NULL CHECK (role IN ('OWNER', 'ADMINISTRATOR', 'OPERATOR', 'MEMBER', 'AUDITOR')),
    permissions text[] NOT NULL DEFAULT '{}',
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE UNIQUE INDEX memberships_platform_one
    ON control_plane.memberships (organization_id, subject_id)
    WHERE project_id IS NULL;

CREATE TABLE control_plane.projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    purpose text NOT NULL DEFAULT '' CHECK (char_length(purpose) <= 2000),
    language text NOT NULL DEFAULT 'ru' CHECK (language IN ('ru', 'en')),
    lifecycle text NOT NULL DEFAULT 'ACTIVE' CHECK (lifecycle IN ('ACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, name)
);

ALTER TABLE control_plane.memberships
    ADD CONSTRAINT memberships_project_fk
    FOREIGN KEY (project_id) REFERENCES control_plane.projects(id);

CREATE UNIQUE INDEX memberships_project_one
    ON control_plane.memberships (project_id, subject_id)
    WHERE project_id IS NOT NULL;

CREATE TABLE control_plane.platform_capabilities (
    stable_key text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL,
    risk text NOT NULL CHECK (risk IN ('LOW', 'MEDIUM', 'HIGH')),
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE control_plane.provider_definitions (
    stable_key text PRIMARY KEY CHECK (stable_key ~ '^[a-z][a-z0-9_-]{1,95}$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    adapter_key text NOT NULL UNIQUE CHECK (adapter_key ~ '^[a-z][a-z0-9_-]{1,95}$'),
    capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE control_plane.provider_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    definition_key text NOT NULL REFERENCES control_plane.provider_definitions(stable_key),
    stable_key text NOT NULL CHECK (stable_key ~ '^[a-z][a-z0-9_-]{1,95}$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    external_account_masked text NOT NULL DEFAULT '' CHECK (char_length(external_account_masked) <= 320),
    state text NOT NULL CHECK (state IN ('PENDING_AUTHORIZATION', 'AUTHORIZED', 'REAUTHORIZATION_REQUIRED', 'REVOKED')),
    enabled boolean NOT NULL DEFAULT true,
    current_credential_revision_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, stable_key),
    UNIQUE (organization_id, name)
);

CREATE TABLE control_plane.provider_credential_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    revision_number bigint NOT NULL CHECK (revision_number > 0),
    secret_name text NOT NULL CHECK (secret_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND char_length(secret_name) <= 63),
    secret_uid uuid NOT NULL,
    secret_resource_version text NOT NULL CHECK (char_length(secret_resource_version) BETWEEN 1 AND 128),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (provider_account_id, revision_number),
    UNIQUE (provider_account_id, secret_uid, secret_resource_version)
);

ALTER TABLE control_plane.provider_accounts
    ADD CONSTRAINT provider_accounts_current_credential_revision_fk
    FOREIGN KEY (current_credential_revision_id) REFERENCES control_plane.provider_credential_revisions(id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_provider_credential_revision()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'provider credential revision is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_provider_credential_revision
BEFORE UPDATE OR DELETE ON control_plane.provider_credential_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_credential_revision();

CREATE TABLE control_plane.runtime_profiles (
    stable_key text PRIMARY KEY,
    name text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    runtime_revision text NOT NULL,
    resource_limits jsonb NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE control_plane.role_definitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    stable_key text,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    role_type text NOT NULL CHECK (char_length(role_type) BETWEEN 1 AND 96),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 4000),
    default_policies jsonb NOT NULL DEFAULT '{}'::jsonb,
    lifecycle text NOT NULL DEFAULT 'ACTIVE' CHECK (lifecycle IN ('ACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, stable_key),
    UNIQUE NULLS NOT DISTINCT (organization_id, project_id, name)
);

CREATE TABLE control_plane.agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    role_definition_id uuid NOT NULL REFERENCES control_plane.role_definitions(id),
    system_key text,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    purpose text NOT NULL DEFAULT '' CHECK (char_length(purpose) <= 2000),
    role_description text NOT NULL DEFAULT '' CHECK (char_length(role_description) <= 4000),
    avatar_url text NOT NULL DEFAULT '',
    runtime_key text NOT NULL REFERENCES control_plane.runtime_profiles(stable_key),
    state text NOT NULL CHECK (state IN ('DRAFT', 'READY', 'RUNNING', 'DISABLED', 'ARCHIVED')),
    enabled boolean NOT NULL DEFAULT true,
    capabilities text[] NOT NULL DEFAULT '{}',
    external_identities jsonb NOT NULL DEFAULT '[]'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, system_key),
    CHECK ((system_key IS NULL AND project_id IS NOT NULL) OR
           (system_key = 'system-assistant' AND project_id IS NULL AND enabled AND state <> 'ARCHIVED'))
);

CREATE TABLE control_plane.role_image_recipes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgrec_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    role_definition_id uuid NOT NULL REFERENCES control_plane.role_definitions(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    state text NOT NULL CHECK (state IN ('ACTIVE', 'ARCHIVED')),
    specification jsonb NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    spec_sha256 text NOT NULL CHECK (spec_sha256 ~ '^[a-f0-9]{64}$'),
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    policy_sha256 text NOT NULL CHECK (policy_sha256 ~ '^[a-f0-9]{64}$'),
    role_runtime_contract_revision bigint NOT NULL CHECK (role_runtime_contract_revision > 0),
    role_runtime_contract_sha256 text NOT NULL CHECK (role_runtime_contract_sha256 ~ '^[a-f0-9]{64}$'),
    active_image_artifact_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (role_definition_id, name)
);

CREATE TABLE control_plane.image_builds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgbld_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    recipe_id uuid NOT NULL REFERENCES control_plane.role_image_recipes(id),
    recipe_version bigint NOT NULL CHECK (recipe_version > 0),
    recipe_generation bigint NOT NULL CHECK (recipe_generation > 0),
    specification jsonb NOT NULL,
    spec_sha256 text NOT NULL CHECK (spec_sha256 ~ '^[a-f0-9]{64}$'),
    immutable_build_sha256 text NOT NULL CHECK (immutable_build_sha256 ~ '^[a-f0-9]{64}$'),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 10),
    maximum_attempts integer NOT NULL CHECK (maximum_attempts BETWEEN 1 AND 10),
    stage text NOT NULL CHECK (stage IN ('QUEUED', 'MATERIALIZATION', 'CONTEXT_VALIDATION', 'BASE_PULL', 'SOLVING', 'INSTALLATION', 'TRUSTED_RUNTIME_FINALIZATION', 'STAGING_PUSH', 'PROVENANCE', 'COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER')),
    progress_percent integer NOT NULL DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
    claimant_workload text,
    authority_generation bigint NOT NULL DEFAULT 0 CHECK (authority_generation >= 0),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_token_sha256 text CHECK (lease_token_sha256 IS NULL OR lease_token_sha256 ~ '^[a-f0-9]{64}$'),
    lease_expires_at timestamptz,
    staging_reference text NOT NULL DEFAULT '',
    manifest_digest text NOT NULL DEFAULT '' CHECK (manifest_digest = '' OR manifest_digest ~ '^sha256:[a-f0-9]{64}$'),
    provenance_sha256 text NOT NULL DEFAULT '' CHECK (provenance_sha256 = '' OR provenance_sha256 ~ '^[a-f0-9]{64}$'),
    safe_error_code text NOT NULL DEFAULT '',
    diagnostic_code text NOT NULL DEFAULT '',
    diagnostic_summary text NOT NULL DEFAULT '' CHECK (char_length(diagnostic_summary) <= 256),
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((claimant_workload IS NULL AND lease_token_sha256 IS NULL AND lease_expires_at IS NULL) OR
           (claimant_workload IS NOT NULL AND authority_generation > 0 AND fence > 0 AND lease_token_sha256 IS NOT NULL AND lease_expires_at IS NOT NULL))
);

CREATE INDEX image_builds_claimable
    ON control_plane.image_builds (available_at, created_at)
    WHERE stage IN ('QUEUED', 'FAILED', 'EXPIRED');

CREATE TABLE control_plane.image_artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgart_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    recipe_id uuid NOT NULL REFERENCES control_plane.role_image_recipes(id),
    recipe_version bigint NOT NULL CHECK (recipe_version > 0),
    recipe_generation bigint NOT NULL CHECK (recipe_generation > 0),
    spec_sha256 text NOT NULL CHECK (spec_sha256 ~ '^[a-f0-9]{64}$'),
    build_id uuid NOT NULL UNIQUE REFERENCES control_plane.image_builds(id),
    build_version bigint NOT NULL CHECK (build_version > 0),
    build_attempt integer NOT NULL CHECK (build_attempt BETWEEN 1 AND 10),
    specification jsonb NOT NULL,
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    policy_sha256 text NOT NULL CHECK (policy_sha256 ~ '^[a-f0-9]{64}$'),
    role_runtime_contract_revision bigint NOT NULL CHECK (role_runtime_contract_revision > 0),
    role_runtime_contract_sha256 text NOT NULL CHECK (role_runtime_contract_sha256 ~ '^[a-f0-9]{64}$'),
    staging_reference text NOT NULL,
    manifest_digest text NOT NULL CHECK (manifest_digest ~ '^sha256:[a-f0-9]{64}$'),
    immutable_build_sha256 text NOT NULL CHECK (immutable_build_sha256 ~ '^[a-f0-9]{64}$'),
    provenance_sha256 text NOT NULL CHECK (provenance_sha256 ~ '^[a-f0-9]{64}$'),
    admission_state text NOT NULL DEFAULT 'PENDING' CHECK (admission_state IN ('PENDING', 'CLAIMED', 'ACCEPTED', 'REJECTED')),
    admission_claimant_workload text,
    admission_authority_generation bigint NOT NULL DEFAULT 0 CHECK (admission_authority_generation >= 0),
    admission_fence bigint NOT NULL DEFAULT 0 CHECK (admission_fence >= 0),
    admission_claim_token_sha256 text CHECK (admission_claim_token_sha256 IS NULL OR admission_claim_token_sha256 ~ '^[a-f0-9]{64}$'),
    admission_claim_expires_at timestamptz,
    sbom_sha256 text NOT NULL DEFAULT '',
    vulnerability_evidence_sha256 text NOT NULL DEFAULT '',
    admission_verdict text NOT NULL DEFAULT '' CHECK (admission_verdict IN ('', 'ACCEPTED', 'REJECTED')),
    signature_identity text NOT NULL DEFAULT '',
    signature_sha256 text NOT NULL DEFAULT '',
    admission_revision bigint NOT NULL DEFAULT 0 CHECK (admission_revision >= 0),
    admission_receipt_sha256 text NOT NULL DEFAULT '',
    admission_receipt_oci_manifest_digest text NOT NULL DEFAULT '',
    promotion_state text NOT NULL DEFAULT 'PENDING' CHECK (promotion_state IN ('PENDING', 'CLAIMED', 'AUTHORIZED', 'PROMOTED', 'REJECTED')),
    promotion_claimant_workload text,
    promotion_authority_generation bigint NOT NULL DEFAULT 0 CHECK (promotion_authority_generation >= 0),
    promotion_fence bigint NOT NULL DEFAULT 0 CHECK (promotion_fence >= 0),
    promotion_claim_token_sha256 text CHECK (promotion_claim_token_sha256 IS NULL OR promotion_claim_token_sha256 ~ '^[a-f0-9]{64}$'),
    promotion_claim_expires_at timestamptz,
    promotion_authorization_token_sha256 text CHECK (promotion_authorization_token_sha256 IS NULL OR promotion_authorization_token_sha256 ~ '^[a-f0-9]{64}$'),
    promotion_authorization_expires_at timestamptz,
    promoted_reference text NOT NULL DEFAULT '',
    promotion_readback_sha256 text NOT NULL DEFAULT '',
    promoted_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE control_plane.role_image_recipes
    ADD CONSTRAINT role_image_recipes_active_artifact_fk
    FOREIGN KEY (active_image_artifact_id) REFERENCES control_plane.image_artifacts(id);

CREATE INDEX image_artifacts_admission_claimable
    ON control_plane.image_artifacts (created_at)
    WHERE admission_state IN ('PENDING', 'CLAIMED');

CREATE INDEX image_artifacts_promotion_claimable
    ON control_plane.image_artifacts (created_at)
    WHERE admission_state = 'ACCEPTED' AND promotion_state IN ('PENDING', 'CLAIMED');

CREATE TABLE control_plane.instruction_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    version_number integer NOT NULL CHECK (version_number > 0),
    state text NOT NULL CHECK (state IN ('DRAFT', 'VALID', 'INVALID', 'PUBLISHED')),
    content text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 100000),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    validation_problems jsonb NOT NULL DEFAULT '[]'::jsonb,
    core boolean NOT NULL DEFAULT false,
    parent_ref text,
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    published_at timestamptz,
    UNIQUE (agent_id, version_number),
    UNIQUE (agent_id, digest),
    CHECK ((state = 'PUBLISHED') = (published_at IS NOT NULL))
);

CREATE UNIQUE INDEX instruction_one_draft
    ON control_plane.instruction_versions (agent_id)
    WHERE state IN ('DRAFT', 'VALID', 'INVALID');

CREATE TABLE control_plane.workflows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    purpose text NOT NULL DEFAULT '' CHECK (char_length(purpose) <= 2000),
    coordinator_agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    state text NOT NULL CHECK (state IN ('DRAFT', 'VALID', 'PUBLISHED', 'ARCHIVED')),
    draft_spec jsonb NOT NULL,
    published_spec jsonb,
    published_version integer NOT NULL DEFAULT 0 CHECK (published_version >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (project_id, name)
);

CREATE TABLE control_plane.workflow_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    workflow_id uuid NOT NULL REFERENCES control_plane.workflows(id),
    version_number integer NOT NULL CHECK (version_number > 0),
    spec jsonb NOT NULL,
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workflow_id, version_number),
    UNIQUE (workflow_id, digest)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_immutable_row()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'immutable row cannot be changed';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_workflow_version
BEFORE UPDATE OR DELETE ON control_plane.workflow_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    target_type text NOT NULL CHECK (target_type IN ('AGENT', 'WORKFLOW', 'SYSTEM_ASSISTANT')),
    target_ref text NOT NULL,
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    state text NOT NULL CHECK (state IN ('ACTIVE', 'CLOSED')),
    next_turn_number bigint NOT NULL DEFAULT 1 CHECK (next_turn_number > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_session_provider_account()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.provider_account_id IS DISTINCT FROM OLD.provider_account_id THEN
        RAISE EXCEPTION 'session provider account is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_session_provider_account
BEFORE UPDATE OF provider_account_id ON control_plane.sessions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_session_provider_account();

CREATE TABLE control_plane.runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    session_id uuid REFERENCES control_plane.sessions(id),
    root_run_id uuid REFERENCES control_plane.runs(id),
    parent_run_id uuid REFERENCES control_plane.runs(id),
    retry_of_run_id uuid REFERENCES control_plane.runs(id),
    target_type text NOT NULL CHECK (target_type IN ('AGENT', 'WORKFLOW', 'SYSTEM_ASSISTANT')),
    target_ref text NOT NULL,
    source text NOT NULL CHECK (source IN ('CONTROL_CENTER', 'SYSTEM_ASSISTANT', 'SCHEDULE', 'INTEGRATION', 'AGENT_DELEGATION', 'MATTERMOST')),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    task text NOT NULL CHECK (char_length(task) BETWEEN 1 AND 20000),
    input jsonb NOT NULL DEFAULT '{}'::jsonb,
    input_artifact_refs text[] NOT NULL DEFAULT '{}',
    state text NOT NULL CHECK (state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    concurrency_limit integer NOT NULL DEFAULT 1 CHECK (concurrency_limit BETWEEN 1 AND 100),
    graph_revision bigint NOT NULL DEFAULT 1 CHECK (graph_revision > 0),
    event_sequence bigint NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    result_summary text NOT NULL DEFAULT '',
    safe_error_code text NOT NULL DEFAULT '',
    safe_error_message text NOT NULL DEFAULT '',
    usage jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    initiated_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    finished_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX runs_project_recent ON control_plane.runs (project_id, created_at DESC);
CREATE INDEX runs_root ON control_plane.runs (root_run_id, created_at);

CREATE TABLE control_plane.session_turns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    run_id uuid REFERENCES control_plane.runs(id),
    turn_number bigint NOT NULL CHECK (turn_number > 0),
    actor_kind text NOT NULL CHECK (actor_kind IN ('USER', 'AGENT', 'SYSTEM_ASSISTANT')),
    actor_ref text NOT NULL,
    content text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 100000),
    artifact_refs text[] NOT NULL DEFAULT '{}',
    state text NOT NULL CHECK (state IN ('QUEUED', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    completed_at timestamptz,
    UNIQUE (session_id, turn_number)
);

CREATE TABLE control_plane.run_nodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    parent_node_id uuid REFERENCES control_plane.run_nodes(id),
    type text NOT NULL CHECK (type IN ('ROOT_PROCESS', 'AGENT_EXECUTION', 'HUMAN_GATE', 'EXTERNAL_ACTION')),
    state text NOT NULL CHECK (state IN ('QUEUED', 'RUNNING', 'WAITING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'SKIPPED')),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 160),
    role text NOT NULL DEFAULT '',
    agent_id uuid REFERENCES control_plane.agents(id),
    turn_id uuid REFERENCES control_plane.session_turns(id),
    workflow_step_key text NOT NULL DEFAULT '',
    human_gate_after boolean NOT NULL DEFAULT false,
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    input_summary text NOT NULL DEFAULT '',
    progress_summary text NOT NULL DEFAULT '',
    integration_names text[] NOT NULL DEFAULT '{}',
    callback_summary text NOT NULL DEFAULT '',
    safe_error_code text NOT NULL DEFAULT '',
    safe_error_message text NOT NULL DEFAULT '',
    next_actions text[] NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    finished_at timestamptz
);

CREATE INDEX run_nodes_graph ON control_plane.run_nodes (root_run_id, created_at);

CREATE TABLE control_plane.runtime_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^rrev_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    turn_id uuid REFERENCES control_plane.session_turns(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    role_definition_id uuid NOT NULL REFERENCES control_plane.role_definitions(id),
    role_image_recipe_id uuid REFERENCES control_plane.role_image_recipes(id),
    role_image_artifact_id uuid REFERENCES control_plane.image_artifacts(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    provider_credential_revision_id uuid NOT NULL REFERENCES control_plane.provider_credential_revisions(id),
    generation bigint NOT NULL CHECK (generation > 0),
    attempt integer NOT NULL CHECK (attempt > 0),
    runtime_profile_key text NOT NULL REFERENCES control_plane.runtime_profiles(stable_key),
    runtime_profile_revision text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    provider_account_ref text NOT NULL,
    provider_credential_revision_ref text NOT NULL,
    provider_credential_revision_number bigint NOT NULL CHECK (provider_credential_revision_number > 0),
    provider_secret_name text NOT NULL CHECK (provider_secret_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND char_length(provider_secret_name) <= 63),
    provider_secret_uid uuid NOT NULL,
    provider_secret_resource_version text NOT NULL CHECK (char_length(provider_secret_resource_version) BETWEEN 1 AND 128),
    provider_credential_sha256 text NOT NULL CHECK (provider_credential_sha256 ~ '^[a-f0-9]{64}$'),
    instruction_ref text NOT NULL,
    instruction_digest text NOT NULL CHECK (instruction_digest ~ '^[a-f0-9]{64}$'),
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    capabilities text[] NOT NULL,
    integration_grants_digest text NOT NULL CHECK (integration_grants_digest ~ '^[a-f0-9]{64}$'),
    image_reference text NOT NULL CHECK (image_reference ~ '@sha256:[a-f0-9]{64}$'),
    image_manifest_digest text NOT NULL CHECK (image_manifest_digest ~ '^sha256:[a-f0-9]{64}$'),
    role_runtime_contract_revision bigint NOT NULL CHECK (role_runtime_contract_revision > 0),
    role_runtime_contract_sha256 text NOT NULL CHECK (role_runtime_contract_sha256 ~ '^[a-f0-9]{64}$'),
    revision_digest text NOT NULL CHECK (revision_digest ~ '^[a-f0-9]{64}$'),
    safe_snapshot jsonb NOT NULL CHECK (octet_length(safe_snapshot::text) <= 262144),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (node_id, generation)
);

CREATE TRIGGER protect_runtime_revision
BEFORE UPDATE OR DELETE ON control_plane.runtime_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.run_edges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    source_node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    target_node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    type text NOT NULL CHECK (type IN ('DELEGATED_TO', 'CALLBACK_TO', 'RETRY_OF', 'CONTINUES', 'WAITING_FOR')),
    label text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (root_run_id, source_node_id, target_node_id, type)
);

CREATE TABLE control_plane.run_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    aggregate_ref text NOT NULL,
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    sequence bigint NOT NULL CHECK (sequence > 0),
    type text NOT NULL CHECK (type IN ('RUN_CREATED', 'RUN_STATE_CHANGED', 'NODE_ADDED', 'NODE_STATE_CHANGED', 'EDGE_ADDED', 'TURN_QUEUED', 'TURN_STARTED', 'TURN_PROGRESS', 'TURN_COMPLETED', 'DELEGATION_CREATED', 'CALLBACK_DELIVERED', 'OWNER_GATE_OPENED', 'OWNER_GATE_RESOLVED', 'ARTIFACT_AVAILABLE', 'INCIDENT_LINKED')),
    node_ref text,
    edge_ref text,
    gate_ref text,
    artifact_ref text,
    safe_summary text NOT NULL CHECK (char_length(safe_summary) <= 2000),
    safe_progress text NOT NULL DEFAULT '' CHECK (char_length(safe_progress) <= 2000),
    run_state text,
    node_state text,
    safe_delta jsonb NOT NULL CHECK (jsonb_typeof(safe_delta) = 'object' AND octet_length(safe_delta::text) <= 49152),
    correlation_ref text NOT NULL,
    causation_ref text,
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (root_run_id, sequence)
);

CREATE TABLE control_plane.owner_gates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    title text NOT NULL,
    prompt text NOT NULL,
    context_summary text NOT NULL DEFAULT '',
    allowed_decisions text[] NOT NULL,
    state text NOT NULL CHECK (state IN ('OPEN', 'APPROVED', 'REJECTED', 'CHANGES_REQUESTED', 'CANCELLED')),
    decision text,
    decision_comment text NOT NULL DEFAULT '',
    resolved_by uuid REFERENCES control_plane.subjects(id),
    resolved_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((state = 'OPEN' AND decision IS NULL AND resolved_by IS NULL AND resolved_at IS NULL) OR
           (state <> 'OPEN' AND decision IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL))
);

CREATE INDEX owner_gates_open ON control_plane.owner_gates (project_id, created_at) WHERE state = 'OPEN';

CREATE TABLE control_plane.artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    run_id uuid REFERENCES control_plane.runs(id),
    node_id uuid REFERENCES control_plane.run_nodes(id),
    file_name text NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 255),
    media_type text NOT NULL CHECK (char_length(media_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 1073741824),
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    source text NOT NULL CHECK (source IN ('CONTROL_CENTER', 'AGENT_RESULT', 'INTEGRATION_RESULT', 'KNOWLEDGE_SOURCE', 'INTERACTION_ATTACHMENT')),
    scan_state text NOT NULL CHECK (scan_state IN ('PENDING', 'SCANNING', 'CLEAN', 'QUARANTINED', 'FAILED')),
    object_receipt_ref text NOT NULL,
    preview_state text NOT NULL CHECK (preview_state IN ('AVAILABLE', 'UNAVAILABLE', 'BLOCKED')),
    revision bigint NOT NULL CHECK (revision > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE UNIQUE INDEX artifacts_project_file_revision
    ON control_plane.artifacts (project_id, file_name, revision);

CREATE TABLE control_plane.artifact_bindings (
    artifact_id uuid NOT NULL REFERENCES control_plane.artifacts(id),
    target_kind text NOT NULL CHECK (target_kind IN ('AGENT', 'WORKFLOW', 'RUN_RESULT', 'RUN_INPUT', 'KNOWLEDGE')),
    target_ref text NOT NULL,
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (artifact_id, target_kind, target_ref)
);

CREATE TABLE control_plane.artifact_content (
    artifact_id uuid PRIMARY KEY REFERENCES control_plane.artifacts(id) ON DELETE CASCADE,
    body bytea NOT NULL CHECK (octet_length(body) <= 16777216)
);

CREATE TABLE control_plane.artifact_download_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    artifact_id uuid NOT NULL REFERENCES control_plane.artifacts(id) ON DELETE CASCADE,
    artifact_version bigint NOT NULL CHECK (artifact_version > 0),
    subject_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    purpose text NOT NULL CHECK (purpose IN ('DOWNLOAD', 'PREVIEW')),
    issued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CHECK (expires_at > issued_at),
    CHECK (consumed_at IS NULL OR consumed_at >= issued_at)
);

CREATE INDEX artifact_download_grants_expiry
    ON control_plane.artifact_download_grants (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE control_plane.schedules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    name text NOT NULL,
    target_type text NOT NULL CHECK (target_type IN ('AGENT', 'WORKFLOW')),
    target_ref text NOT NULL,
    preset text NOT NULL,
    cron_expression text NOT NULL,
    timezone text NOT NULL,
    input jsonb NOT NULL DEFAULT '{}'::jsonb,
    session_policy text NOT NULL,
    notification_policy text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    next_run_at timestamptz,
    last_run_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (project_id, name)
);

CREATE TABLE control_plane.integration_definitions (
    stable_key text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL,
    category text NOT NULL,
    optional boolean NOT NULL DEFAULT true CHECK (optional),
    enabled boolean NOT NULL DEFAULT true,
    capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
    configuration_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE control_plane.integration_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    definition_key text NOT NULL REFERENCES control_plane.integration_definitions(stable_key),
    name text NOT NULL,
    state text NOT NULL CHECK (state IN ('NOT_CONNECTED', 'TESTING', 'CONNECTED', 'DEGRADED', 'DISABLED')),
    enabled boolean NOT NULL DEFAULT true,
    credential_materialization_ref text NOT NULL,
    masked_credentials_state text NOT NULL CHECK (masked_credentials_state IN ('NOT_CONFIGURED', 'CONFIGURED', 'INVALID')),
    public_configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_test_summary text NOT NULL DEFAULT '',
    last_tested_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, name)
);

CREATE TABLE control_plane.integration_connection_tests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    state text NOT NULL CHECK (state IN ('DUE', 'CLAIMED', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    lease_ref text UNIQUE,
    fence_digest text,
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    workload_instance text,
    lease_expires_at timestamptz,
    result_summary text NOT NULL DEFAULT '',
    safe_error_code text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((state = 'CLAIMED' AND lease_ref IS NOT NULL AND fence_digest IS NOT NULL AND workload_instance IS NOT NULL AND lease_expires_at IS NOT NULL) OR
           (state <> 'CLAIMED' AND lease_ref IS NULL AND fence_digest IS NULL AND workload_instance IS NULL AND lease_expires_at IS NULL))
);

CREATE UNIQUE INDEX integration_connection_tests_active_idx
    ON control_plane.integration_connection_tests(connection_id)
    WHERE state IN ('DUE', 'CLAIMED');

CREATE TABLE control_plane.integration_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    capability_key text NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('AGENT', 'WORKFLOW')),
    target_ref text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    approval_policy text NOT NULL DEFAULT 'OWNER_FOR_HIGH_RISK',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (connection_id, capability_key, target_kind, target_ref)
);

CREATE TABLE control_plane.assistant_runtime (
    organization_id uuid PRIMARY KEY REFERENCES control_plane.organizations(id),
    agent_id uuid NOT NULL UNIQUE REFERENCES control_plane.agents(id),
    stable_key text NOT NULL UNIQUE CHECK (stable_key = 'system-assistant'),
    core_prompt_ref text NOT NULL REFERENCES control_plane.instruction_versions(ref),
    core_prompt_revision text NOT NULL,
    owner_instructions text NOT NULL DEFAULT '',
    runtime_state text NOT NULL CHECK (runtime_state IN ('STARTING', 'READY', 'BUSY', 'RECOVERING', 'UNAVAILABLE')),
    runtime_revision text NOT NULL,
    desired_runtime_revision text NOT NULL,
    system_session_ref text NOT NULL REFERENCES control_plane.sessions(ref),
    warm_instance_ref text,
    resource_limits jsonb NOT NULL,
    last_heartbeat_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE control_plane.assistant_plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    conversation_ref text NOT NULL,
    summary text NOT NULL,
    operations jsonb NOT NULL,
    state text NOT NULL CHECK (state IN ('PROPOSED', 'APPLYING', 'APPLIED', 'REJECTED', 'FAILED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    applied_at timestamptz
);

CREATE TABLE control_plane.assistant_conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    session_id uuid NOT NULL UNIQUE REFERENCES control_plane.sessions(id),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    state text NOT NULL CHECK (state IN ('ACTIVE', 'CLOSED')),
    latest_plan_id uuid REFERENCES control_plane.assistant_plans(id),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE control_plane.assistant_plans
    ADD CONSTRAINT assistant_plans_conversation_fk
    FOREIGN KEY (conversation_ref) REFERENCES control_plane.assistant_conversations(ref);

CREATE TABLE control_plane.schedule_occurrences (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    schedule_id uuid NOT NULL REFERENCES control_plane.schedules(id),
    scheduled_for timestamptz NOT NULL,
    state text NOT NULL CHECK (state IN ('DUE', 'CLAIMED', 'MATERIALIZED', 'COMPLETED', 'FAILED', 'CANCELLED')),
    run_id uuid REFERENCES control_plane.runs(id),
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    lease_ref text UNIQUE,
    fence_digest text,
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    workload_instance text,
    lease_expires_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (schedule_id, scheduled_for, attempt)
);

CREATE TABLE control_plane.integration_invocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    grant_id uuid NOT NULL REFERENCES control_plane.integration_grants(id),
    capability_key text NOT NULL,
    operation text NOT NULL,
    idempotency_key text NOT NULL,
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    bounded_input jsonb NOT NULL,
    effect_fence_digest text CHECK (effect_fence_digest IS NULL OR effect_fence_digest ~ '^[a-f0-9]{64}$'),
    lease_ref text UNIQUE,
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    workload_instance text,
    lease_expires_at timestamptz,
    state text NOT NULL CHECK (state IN ('READY', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
    result_summary text NOT NULL DEFAULT '',
    safe_error_code text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (node_id, idempotency_key),
    CHECK ((state = 'RUNNING' AND lease_ref IS NOT NULL AND effect_fence_digest IS NOT NULL AND workload_instance IS NOT NULL AND lease_expires_at IS NOT NULL) OR
           (state <> 'RUNNING' AND lease_ref IS NULL AND effect_fence_digest IS NULL AND workload_instance IS NULL AND lease_expires_at IS NULL))
);

CREATE TABLE control_plane.runtime_leases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id),
    workload_instance text NOT NULL,
    fence_digest text NOT NULL CHECK (fence_digest ~ '^[a-f0-9]{64}$'),
    generation bigint NOT NULL CHECK (generation > 0),
    state text NOT NULL CHECK (state IN ('CLAIMED', 'COMPLETED', 'CANCELLED', 'EXPIRED')),
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (node_id, generation)
);

CREATE TABLE control_plane.callback_receipts (
    child_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    callback_edge_id uuid NOT NULL REFERENCES control_plane.run_edges(id),
    delivered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (child_run_id, callback_edge_id)
);

CREATE TABLE control_plane.idempotency_receipts (
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    operation text NOT NULL,
    idempotency_key text NOT NULL,
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    response_type text NOT NULL,
    response_payload bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, actor_id, operation, idempotency_key)
);

CREATE TABLE control_plane.audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    assistant_agent_id uuid REFERENCES control_plane.agents(id),
    action text NOT NULL,
    resource_kind text NOT NULL,
    resource_ref text NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('SUCCEEDED', 'REJECTED', 'FAILED', 'CONFLICT')),
    safe_summary text NOT NULL,
    correlation_ref text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX audit_events_scope ON control_plane.audit_events (organization_id, project_id, occurred_at DESC);

CREATE TABLE control_plane.outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL UNIQUE,
    subject text NOT NULL,
    ordering_key text NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    payload bytea NOT NULL CHECK (octet_length(payload) <= 65536),
    state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'CLAIMED', 'PUBLISHED', 'DEAD_LETTER')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_owner text,
    lease_expires_at timestamptz,
    broker_receipt text,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (ordering_key, sequence)
);

CREATE INDEX outbox_pending ON control_plane.outbox_events (available_at, created_at)
    WHERE state IN ('PENDING', 'CLAIMED');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_system_agent()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' AND OLD.system_key = 'system-assistant' THEN
        RAISE EXCEPTION 'system assistant is protected';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.system_key = 'system-assistant' AND
       (NEW.system_key IS DISTINCT FROM OLD.system_key OR NEW.enabled = false OR
        NEW.state IN ('DISABLED', 'ARCHIVED') OR NEW.project_id IS NOT NULL) THEN
        RAISE EXCEPTION 'system assistant invariant violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_system_agent
BEFORE UPDATE OR DELETE ON control_plane.agents
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_system_agent();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_core_prompt()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.core OR OLD.state = 'PUBLISHED' THEN
        RAISE EXCEPTION 'published instruction is immutable';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_core_prompt
BEFORE UPDATE OR DELETE ON control_plane.instruction_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_core_prompt();

INSERT INTO control_plane.installation (singleton, schema_version)
VALUES (true, 1)
ON CONFLICT (singleton) DO NOTHING;

-- +goose Down
DROP SCHEMA IF EXISTS control_plane CASCADE;
