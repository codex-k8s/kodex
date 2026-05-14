-- +goose Up
CREATE TABLE agent_manager_flows (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    slug text NOT NULL,
    display_name jsonb NOT NULL DEFAULT '[]'::jsonb,
    description jsonb NOT NULL DEFAULT '[]'::jsonb,
    icon_object_uri text NOT NULL DEFAULT '',
    status text NOT NULL,
    active_version_id uuid,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_flows_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository')),
    CONSTRAINT agent_manager_flows_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT agent_manager_flows_slug_chk CHECK (slug <> ''),
    CONSTRAINT agent_manager_flows_display_name_chk CHECK (jsonb_typeof(display_name) = 'array'),
    CONSTRAINT agent_manager_flows_description_chk CHECK (jsonb_typeof(description) = 'array'),
    CONSTRAINT agent_manager_flows_status_chk
        CHECK (status IN ('draft', 'active', 'disabled', 'archived')),
    CONSTRAINT agent_manager_flows_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX agent_manager_flows_scope_slug_uidx
    ON agent_manager_flows (scope_type, scope_ref, slug);

CREATE INDEX agent_manager_flows_scope_status_slug_idx
    ON agent_manager_flows (scope_type, scope_ref, status, slug);

CREATE TABLE agent_manager_flow_versions (
    id uuid PRIMARY KEY,
    flow_id uuid NOT NULL REFERENCES agent_manager_flows(id),
    version bigint NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    definition_digest text NOT NULL,
    status text NOT NULL,
    activated_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (flow_id, id),
    UNIQUE (flow_id, version),
    CONSTRAINT agent_manager_flow_versions_version_chk CHECK (version > 0),
    CONSTRAINT agent_manager_flow_versions_digest_chk CHECK (definition_digest <> ''),
    CONSTRAINT agent_manager_flow_versions_status_chk
        CHECK (status IN ('draft', 'active', 'superseded', 'rejected'))
);

ALTER TABLE agent_manager_flows
    ADD CONSTRAINT agent_manager_flows_active_version_fk
        FOREIGN KEY (id, active_version_id)
        REFERENCES agent_manager_flow_versions(flow_id, id);

CREATE INDEX agent_manager_flow_versions_flow_status_version_idx
    ON agent_manager_flow_versions (flow_id, status, version DESC);

CREATE TABLE agent_manager_stages (
    id uuid PRIMARY KEY,
    flow_version_id uuid NOT NULL REFERENCES agent_manager_flow_versions(id),
    slug text NOT NULL,
    stage_type text NOT NULL,
    display_name jsonb NOT NULL DEFAULT '[]'::jsonb,
    icon_object_uri text NOT NULL DEFAULT '',
    required_artifacts jsonb NOT NULL DEFAULT '{}'::jsonb,
    acceptance_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
    position integer NOT NULL,
    CONSTRAINT agent_manager_stages_slug_chk CHECK (slug <> ''),
    CONSTRAINT agent_manager_stages_type_chk
        CHECK (stage_type IN ('work', 'review', 'gate', 'release', 'ops', 'custom')),
    CONSTRAINT agent_manager_stages_display_name_chk CHECK (jsonb_typeof(display_name) = 'array'),
    CONSTRAINT agent_manager_stages_required_artifacts_chk CHECK (jsonb_typeof(required_artifacts) = 'object'),
    CONSTRAINT agent_manager_stages_acceptance_policy_chk CHECK (jsonb_typeof(acceptance_policy) = 'object'),
    CONSTRAINT agent_manager_stages_position_chk CHECK (position >= 0),
    UNIQUE (flow_version_id, id),
    UNIQUE (flow_version_id, slug),
    UNIQUE (flow_version_id, position)
);

CREATE INDEX agent_manager_stages_flow_version_position_idx
    ON agent_manager_stages (flow_version_id, position);

CREATE TABLE agent_manager_stage_transitions (
    id uuid PRIMARY KEY,
    flow_version_id uuid NOT NULL REFERENCES agent_manager_flow_versions(id),
    from_stage_id uuid,
    to_stage_id uuid NOT NULL,
    condition_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    follow_up_type text NOT NULL DEFAULT '',
    position integer NOT NULL,
    CONSTRAINT agent_manager_stage_transitions_from_stage_fk
        FOREIGN KEY (flow_version_id, from_stage_id)
        REFERENCES agent_manager_stages(flow_version_id, id),
    CONSTRAINT agent_manager_stage_transitions_to_stage_fk
        FOREIGN KEY (flow_version_id, to_stage_id)
        REFERENCES agent_manager_stages(flow_version_id, id),
    CONSTRAINT agent_manager_stage_transitions_condition_chk CHECK (jsonb_typeof(condition_payload) = 'object'),
    CONSTRAINT agent_manager_stage_transitions_position_chk CHECK (position >= 0),
    UNIQUE (flow_version_id, position)
);

CREATE INDEX agent_manager_stage_transitions_flow_version_idx
    ON agent_manager_stage_transitions (flow_version_id, position);

CREATE TABLE agent_manager_role_profiles (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    slug text NOT NULL,
    display_name jsonb NOT NULL DEFAULT '[]'::jsonb,
    icon_object_uri text NOT NULL DEFAULT '',
    role_kind text NOT NULL,
    runtime_profile text NOT NULL,
    allowed_mcp_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    provider_account_policy_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_role_profiles_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository')),
    CONSTRAINT agent_manager_role_profiles_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT agent_manager_role_profiles_slug_chk CHECK (slug <> ''),
    CONSTRAINT agent_manager_role_profiles_display_name_chk CHECK (jsonb_typeof(display_name) = 'array'),
    CONSTRAINT agent_manager_role_profiles_role_kind_chk
        CHECK (role_kind IN ('worker', 'reviewer', 'gatekeeper', 'manager', 'qa', 'ops', 'custom')),
    CONSTRAINT agent_manager_role_profiles_runtime_profile_chk CHECK (runtime_profile <> ''),
    CONSTRAINT agent_manager_role_profiles_allowed_tools_chk CHECK (jsonb_typeof(allowed_mcp_tools) = 'array'),
    CONSTRAINT agent_manager_role_profiles_status_chk
        CHECK (status IN ('draft', 'active', 'disabled', 'archived')),
    CONSTRAINT agent_manager_role_profiles_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX agent_manager_role_profiles_scope_slug_uidx
    ON agent_manager_role_profiles (scope_type, scope_ref, slug);

CREATE INDEX agent_manager_role_profiles_scope_status_kind_idx
    ON agent_manager_role_profiles (scope_type, scope_ref, status, role_kind, slug);

CREATE TABLE agent_manager_stage_role_bindings (
    id uuid PRIMARY KEY,
    stage_id uuid NOT NULL REFERENCES agent_manager_stages(id),
    role_profile_id uuid NOT NULL REFERENCES agent_manager_role_profiles(id),
    binding_kind text NOT NULL,
    launch_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
    required_for_acceptance boolean NOT NULL DEFAULT false,
    CONSTRAINT agent_manager_stage_role_bindings_kind_chk
        CHECK (binding_kind IN ('executor', 'reviewer', 'gatekeeper', 'qa', 'observer', 'custom')),
    CONSTRAINT agent_manager_stage_role_bindings_launch_policy_chk CHECK (jsonb_typeof(launch_policy) = 'object'),
    UNIQUE (stage_id, role_profile_id, binding_kind)
);

CREATE INDEX agent_manager_stage_role_bindings_role_idx
    ON agent_manager_stage_role_bindings (role_profile_id, binding_kind);

CREATE TABLE agent_manager_prompt_templates (
    id uuid PRIMARY KEY,
    role_profile_id uuid NOT NULL REFERENCES agent_manager_role_profiles(id),
    prompt_kind text NOT NULL,
    active_version_id uuid,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (role_profile_id, prompt_kind),
    UNIQUE (role_profile_id, id),
    CONSTRAINT agent_manager_prompt_templates_kind_chk
        CHECK (prompt_kind IN ('work', 'revise', 'review', 'manager', 'custom')),
    CONSTRAINT agent_manager_prompt_templates_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_prompt_templates_role_kind_idx
    ON agent_manager_prompt_templates (role_profile_id, prompt_kind);

CREATE TABLE agent_manager_prompt_template_versions (
    id uuid PRIMARY KEY,
    prompt_template_id uuid NOT NULL REFERENCES agent_manager_prompt_templates(id),
    role_profile_id uuid NOT NULL,
    prompt_kind text NOT NULL,
    version bigint NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    template_object_uri text NOT NULL DEFAULT '',
    template_object_digest text NOT NULL DEFAULT '',
    template_object_size_bytes bigint,
    template_digest text NOT NULL,
    status text NOT NULL,
    activated_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (prompt_template_id, id),
    UNIQUE (prompt_template_id, version),
    CONSTRAINT agent_manager_prompt_template_versions_template_role_fk
        FOREIGN KEY (role_profile_id, prompt_template_id)
        REFERENCES agent_manager_prompt_templates(role_profile_id, id),
    CONSTRAINT agent_manager_prompt_template_versions_kind_chk
        CHECK (prompt_kind IN ('work', 'revise', 'review', 'manager', 'custom')),
    CONSTRAINT agent_manager_prompt_template_versions_version_chk CHECK (version > 0),
    CONSTRAINT agent_manager_prompt_template_versions_object_size_chk
        CHECK (template_object_size_bytes IS NULL OR template_object_size_bytes >= 0),
    CONSTRAINT agent_manager_prompt_template_versions_digest_chk CHECK (template_digest <> ''),
    CONSTRAINT agent_manager_prompt_template_versions_status_chk
        CHECK (status IN ('draft', 'active', 'superseded', 'rejected'))
);

ALTER TABLE agent_manager_prompt_templates
    ADD CONSTRAINT agent_manager_prompt_templates_active_version_fk
        FOREIGN KEY (id, active_version_id)
        REFERENCES agent_manager_prompt_template_versions(prompt_template_id, id);

CREATE INDEX agent_manager_prompt_template_versions_role_kind_status_idx
    ON agent_manager_prompt_template_versions (role_profile_id, prompt_kind, status, version DESC);

CREATE TABLE agent_manager_command_results (
    key text PRIMARY KEY,
    command_id uuid,
    idempotency_key text NOT NULL DEFAULT '',
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    result_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_command_results_key_chk CHECK (key <> ''),
    CONSTRAINT agent_manager_command_results_actor_type_chk CHECK (actor_type <> ''),
    CONSTRAINT agent_manager_command_results_actor_id_chk CHECK (actor_id <> ''),
    CONSTRAINT agent_manager_command_results_operation_chk CHECK (operation <> ''),
    CONSTRAINT agent_manager_command_results_aggregate_type_chk
        CHECK (aggregate_type IN ('flow', 'flow_version', 'role_profile', 'prompt_template', 'prompt_template_version')),
    CONSTRAINT agent_manager_command_results_payload_chk CHECK (jsonb_typeof(result_payload) = 'object'),
    CONSTRAINT agent_manager_command_results_identity_chk CHECK (command_id IS NOT NULL OR idempotency_key <> '')
);

CREATE UNIQUE INDEX agent_manager_command_results_command_id_uidx
    ON agent_manager_command_results (actor_type, actor_id, command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX agent_manager_command_results_idempotency_uidx
    ON agent_manager_command_results (operation, actor_type, actor_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX agent_manager_command_results_aggregate_idx
    ON agent_manager_command_results (aggregate_type, aggregate_id);

CREATE TABLE agent_manager_outbox_events (
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
    CONSTRAINT agent_manager_outbox_events_type_chk CHECK (event_type <> ''),
    CONSTRAINT agent_manager_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT agent_manager_outbox_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT agent_manager_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT agent_manager_outbox_events_attempt_count_chk CHECK (attempt_count >= 0),
    CONSTRAINT agent_manager_outbox_events_failure_kind_chk CHECK (failure_kind IN ('', 'transient', 'permanent'))
);

CREATE INDEX agent_manager_outbox_events_ready_idx
    ON agent_manager_outbox_events (next_attempt_at, occurred_at, id)
    WHERE published_at IS NULL AND failed_permanently_at IS NULL;

CREATE INDEX agent_manager_outbox_events_aggregate_idx
    ON agent_manager_outbox_events (aggregate_type, aggregate_id, occurred_at);

-- +goose Down
DROP TABLE IF EXISTS agent_manager_outbox_events;
DROP TABLE IF EXISTS agent_manager_command_results;

ALTER TABLE agent_manager_prompt_templates
    DROP CONSTRAINT IF EXISTS agent_manager_prompt_templates_active_version_fk;
DROP TABLE IF EXISTS agent_manager_prompt_template_versions;
DROP TABLE IF EXISTS agent_manager_prompt_templates;
DROP TABLE IF EXISTS agent_manager_stage_role_bindings;
DROP TABLE IF EXISTS agent_manager_role_profiles;
DROP TABLE IF EXISTS agent_manager_stage_transitions;
DROP TABLE IF EXISTS agent_manager_stages;

ALTER TABLE agent_manager_flows
    DROP CONSTRAINT IF EXISTS agent_manager_flows_active_version_fk;
DROP TABLE IF EXISTS agent_manager_flow_versions;
DROP TABLE IF EXISTS agent_manager_flows;
