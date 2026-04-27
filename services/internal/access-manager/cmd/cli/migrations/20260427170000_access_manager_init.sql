-- +goose Up
CREATE TABLE access_organizations (
    id uuid PRIMARY KEY,
    kind text NOT NULL,
    slug text NOT NULL UNIQUE,
    display_name text NOT NULL,
    image_asset_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    parent_organization_id uuid REFERENCES access_organizations(id),
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_organizations_kind_check CHECK (kind IN ('owner', 'client', 'contractor', 'saas', 'saas_client', 'saas_contractor')),
    CONSTRAINT access_organizations_status_check CHECK (status IN ('active', 'pending', 'suspended', 'archived')),
    CONSTRAINT access_organizations_owner_not_suspended CHECK (kind <> 'owner' OR status IN ('active', 'pending'))
);

CREATE UNIQUE INDEX access_organizations_one_active_owner_idx
    ON access_organizations ((kind))
    WHERE kind = 'owner' AND status = 'active';

CREATE TABLE access_users (
    id uuid PRIMARY KEY,
    primary_email text NOT NULL UNIQUE,
    display_name text NOT NULL DEFAULT '',
    avatar_asset_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    locale text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_users_status_check CHECK (status IN ('active', 'pending', 'blocked', 'disabled'))
);

CREATE TABLE access_user_identities (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES access_users(id),
    provider text NOT NULL,
    subject text NOT NULL,
    email_at_login text NOT NULL,
    last_login_at timestamptz,
    UNIQUE (provider, subject)
);

CREATE TABLE access_allowlist_entries (
    id uuid PRIMARY KEY,
    match_type text NOT NULL,
    value text NOT NULL,
    organization_id uuid REFERENCES access_organizations(id),
    default_status text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (match_type, value),
    CONSTRAINT access_allowlist_match_type_check CHECK (match_type IN ('email', 'domain')),
    CONSTRAINT access_allowlist_default_status_check CHECK (default_status IN ('active', 'pending')),
    CONSTRAINT access_allowlist_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE access_groups (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_id uuid,
    slug text NOT NULL,
    display_name text NOT NULL,
    parent_group_id uuid REFERENCES access_groups(id),
    image_asset_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (scope_type, scope_id, slug),
    CONSTRAINT access_groups_scope_type_check CHECK (scope_type IN ('global', 'organization')),
    CONSTRAINT access_groups_status_check CHECK (status IN ('active', 'disabled', 'archived')),
    CONSTRAINT access_groups_scope_id_required_check CHECK ((scope_type = 'global' AND scope_id IS NULL) OR (scope_type = 'organization' AND scope_id IS NOT NULL))
);

CREATE TABLE access_memberships (
    id uuid PRIMARY KEY,
    subject_type text NOT NULL,
    subject_id uuid NOT NULL,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    role_hint text NOT NULL DEFAULT '',
    status text NOT NULL,
    source text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (subject_type, subject_id, target_type, target_id),
    CONSTRAINT access_memberships_subject_type_check CHECK (subject_type IN ('user', 'group', 'external_account')),
    CONSTRAINT access_memberships_target_type_check CHECK (target_type IN ('organization', 'group')),
    CONSTRAINT access_memberships_status_check CHECK (status IN ('active', 'pending', 'blocked', 'disabled')),
    CONSTRAINT access_memberships_source_check CHECK (source IN ('manual', 'bootstrap', 'sync', 'system'))
);

CREATE INDEX access_memberships_subject_idx ON access_memberships (subject_type, subject_id, status);
CREATE INDEX access_memberships_target_idx ON access_memberships (target_type, target_id, status);

CREATE TABLE access_actions (
    id uuid PRIMARY KEY,
    key text NOT NULL UNIQUE,
    display_name text NOT NULL,
    description text NOT NULL DEFAULT '',
    resource_type text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_actions_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE access_rules (
    id uuid PRIMARY KEY,
    effect text NOT NULL,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    action_key text NOT NULL REFERENCES access_actions(key),
    resource_type text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    scope_type text NOT NULL,
    scope_id text NOT NULL DEFAULT '',
    priority integer NOT NULL DEFAULT 0,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_rules_effect_check CHECK (effect IN ('allow', 'deny')),
    CONSTRAINT access_rules_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX access_rules_subject_idx ON access_rules (subject_type, subject_id, action_key, resource_type, status);
CREATE INDEX access_rules_scope_idx ON access_rules (scope_type, scope_id, action_key, status);

CREATE TABLE access_external_providers (
    id uuid PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    provider_kind text NOT NULL,
    display_name text NOT NULL,
    icon_asset_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_external_providers_kind_check CHECK (provider_kind IN ('repository', 'identity', 'model', 'messaging', 'payments', 'other')),
    CONSTRAINT access_external_providers_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE access_secret_binding_refs (
    id uuid PRIMARY KEY,
    store_type text NOT NULL,
    store_ref text NOT NULL,
    value_fingerprint text NOT NULL DEFAULT '',
    rotated_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_secret_binding_refs_store_type_check CHECK (store_type IN ('vault', 'kubernetes_secret'))
);

CREATE TABLE access_external_accounts (
    id uuid PRIMARY KEY,
    external_provider_id uuid NOT NULL REFERENCES access_external_providers(id),
    account_type text NOT NULL,
    display_name text NOT NULL,
    image_asset_ref text NOT NULL DEFAULT '',
    owner_scope_type text NOT NULL,
    owner_scope_id text NOT NULL DEFAULT '',
    status text NOT NULL,
    secret_binding_ref_id uuid REFERENCES access_secret_binding_refs(id),
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT access_external_accounts_account_type_check CHECK (account_type IN ('user', 'bot', 'service', 'integration')),
    CONSTRAINT access_external_accounts_status_check CHECK (status IN ('active', 'pending', 'needs_reauth', 'limited', 'blocked', 'disabled'))
);

CREATE INDEX access_external_accounts_owner_idx ON access_external_accounts (owner_scope_type, owner_scope_id, status);
CREATE INDEX access_external_accounts_provider_idx ON access_external_accounts (external_provider_id, status);

CREATE TABLE access_external_account_bindings (
    id uuid PRIMARY KEY,
    external_account_id uuid NOT NULL REFERENCES access_external_accounts(id),
    usage_scope_type text NOT NULL,
    usage_scope_id text NOT NULL,
    allowed_action_keys text[] NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (external_account_id, usage_scope_type, usage_scope_id),
    CONSTRAINT access_external_account_bindings_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX access_external_account_bindings_usage_idx ON access_external_account_bindings (usage_scope_type, usage_scope_id, status);

CREATE TABLE access_decision_audit (
    id uuid PRIMARY KEY,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    action_key text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL DEFAULT '',
    decision text NOT NULL,
    reason_code text NOT NULL,
    policy_version bigint NOT NULL,
    explanation jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT access_decision_audit_decision_check CHECK (decision IN ('allow', 'deny', 'pending'))
);

CREATE INDEX access_decision_audit_subject_idx ON access_decision_audit (subject_type, subject_id, created_at);
CREATE INDEX access_decision_audit_resource_idx ON access_decision_audit (resource_type, resource_id, created_at);
CREATE INDEX access_decision_audit_action_idx ON access_decision_audit (action_key, created_at);

CREATE TABLE access_outbox_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz
);

CREATE INDEX access_outbox_events_unpublished_idx ON access_outbox_events (occurred_at) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS access_outbox_events;
DROP TABLE IF EXISTS access_decision_audit;
DROP TABLE IF EXISTS access_external_account_bindings;
DROP TABLE IF EXISTS access_external_accounts;
DROP TABLE IF EXISTS access_secret_binding_refs;
DROP TABLE IF EXISTS access_external_providers;
DROP TABLE IF EXISTS access_rules;
DROP TABLE IF EXISTS access_actions;
DROP TABLE IF EXISTS access_memberships;
DROP TABLE IF EXISTS access_groups;
DROP TABLE IF EXISTS access_allowlist_entries;
DROP TABLE IF EXISTS access_user_identities;
DROP TABLE IF EXISTS access_users;
DROP TABLE IF EXISTS access_organizations;
