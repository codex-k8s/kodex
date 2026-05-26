-- +goose Up
CREATE TABLE governance_manager_risk_profiles (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    slug text NOT NULL,
    display_name jsonb NOT NULL DEFAULT '[]'::jsonb,
    description jsonb NOT NULL DEFAULT '[]'::jsonb,
    status text NOT NULL,
    active_version bigint,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_risk_profiles_scope_type_chk
        CHECK (scope_type IN (
            'platform', 'organization', 'project', 'repository', 'service', 'path',
            'api_endpoint', 'database_object', 'secret_area', 'runtime_operation',
            'release_line', 'runtime_environment'
        )),
    CONSTRAINT governance_manager_risk_profiles_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT governance_manager_risk_profiles_slug_chk CHECK (slug <> ''),
    CONSTRAINT governance_manager_risk_profiles_display_name_chk CHECK (jsonb_typeof(display_name) = 'array'),
    CONSTRAINT governance_manager_risk_profiles_description_chk CHECK (jsonb_typeof(description) = 'array'),
    CONSTRAINT governance_manager_risk_profiles_status_chk
        CHECK (status IN ('draft', 'active', 'disabled', 'archived')),
    CONSTRAINT governance_manager_risk_profiles_active_version_chk CHECK (active_version IS NULL OR active_version > 0),
    CONSTRAINT governance_manager_risk_profiles_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX governance_manager_risk_profiles_scope_slug_uidx
    ON governance_manager_risk_profiles (scope_type, scope_ref, slug);

CREATE INDEX governance_manager_risk_profiles_scope_status_slug_idx
    ON governance_manager_risk_profiles (scope_type, scope_ref, status, slug);

CREATE TABLE governance_manager_risk_profile_versions (
    risk_profile_id uuid NOT NULL REFERENCES governance_manager_risk_profiles(id),
    profile_version bigint NOT NULL,
    status text NOT NULL,
    content_digest text NOT NULL,
    created_at timestamptz NOT NULL,
    activated_at timestamptz,
    PRIMARY KEY (risk_profile_id, profile_version),
    CONSTRAINT governance_manager_risk_profile_versions_version_chk CHECK (profile_version > 0),
    CONSTRAINT governance_manager_risk_profile_versions_digest_chk CHECK (content_digest <> ''),
    CONSTRAINT governance_manager_risk_profile_versions_status_chk
        CHECK (status IN ('draft', 'active', 'superseded', 'archived'))
);

ALTER TABLE governance_manager_risk_profiles
    ADD CONSTRAINT governance_manager_risk_profiles_active_version_fk
        FOREIGN KEY (id, active_version)
        REFERENCES governance_manager_risk_profile_versions(risk_profile_id, profile_version);

CREATE TABLE governance_manager_gate_policies (
    id uuid PRIMARY KEY,
    risk_profile_id uuid REFERENCES governance_manager_risk_profiles(id),
    profile_version bigint NOT NULL,
    gate_kind text NOT NULL,
    min_risk_class text NOT NULL,
    required_actor_policy_ref text NOT NULL DEFAULT '',
    required_signal_kinds jsonb NOT NULL DEFAULT '[]'::jsonb,
    timeout_policy_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    CONSTRAINT governance_manager_gate_policies_profile_version_chk CHECK (profile_version > 0),
    CONSTRAINT governance_manager_gate_policies_kind_chk
        CHECK (gate_kind IN ('product', 'architecture', 'technical', 'qa', 'release', 'postdeploy', 'emergency', 'custom')),
    CONSTRAINT governance_manager_gate_policies_min_risk_chk CHECK (min_risk_class IN ('R0', 'R1', 'R2', 'R3')),
    CONSTRAINT governance_manager_gate_policies_signal_kinds_chk CHECK (jsonb_typeof(required_signal_kinds) = 'array'),
    CONSTRAINT governance_manager_gate_policies_status_chk CHECK (status IN ('active', 'disabled')),
    CONSTRAINT governance_manager_gate_policies_profile_version_fk
        FOREIGN KEY (risk_profile_id, profile_version)
        REFERENCES governance_manager_risk_profile_versions(risk_profile_id, profile_version)
);

CREATE INDEX governance_manager_gate_policies_profile_version_kind_idx
    ON governance_manager_gate_policies (risk_profile_id, profile_version, status, gate_kind);

CREATE TABLE governance_manager_risk_rules (
    id uuid PRIMARY KEY,
    risk_profile_id uuid NOT NULL REFERENCES governance_manager_risk_profiles(id),
    profile_version bigint NOT NULL,
    rule_kind text NOT NULL,
    matcher jsonb NOT NULL DEFAULT '{}'::jsonb,
    min_risk_class text NOT NULL,
    required_gate_policy_id uuid REFERENCES governance_manager_gate_policies(id),
    reason_template jsonb NOT NULL DEFAULT '[]'::jsonb,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_risk_rules_profile_version_chk CHECK (profile_version > 0),
    CONSTRAINT governance_manager_risk_rules_kind_chk
        CHECK (rule_kind IN ('path', 'service', 'api', 'database', 'secret', 'auth', 'runtime_action', 'release', 'automation', 'document', 'custom')),
    CONSTRAINT governance_manager_risk_rules_matcher_chk CHECK (jsonb_typeof(matcher) = 'object'),
    CONSTRAINT governance_manager_risk_rules_min_risk_chk CHECK (min_risk_class IN ('R0', 'R1', 'R2', 'R3')),
    CONSTRAINT governance_manager_risk_rules_reason_template_chk CHECK (jsonb_typeof(reason_template) = 'array'),
    CONSTRAINT governance_manager_risk_rules_status_chk CHECK (status IN ('active', 'disabled')),
    CONSTRAINT governance_manager_risk_rules_profile_version_fk
        FOREIGN KEY (risk_profile_id, profile_version)
        REFERENCES governance_manager_risk_profile_versions(risk_profile_id, profile_version)
);

CREATE INDEX governance_manager_risk_rules_profile_version_kind_idx
    ON governance_manager_risk_rules (risk_profile_id, profile_version, status, rule_kind);

CREATE TABLE governance_manager_risk_assessments (
    id uuid PRIMARY KEY,
    target_type text NOT NULL,
    target_ref text NOT NULL,
    project_ref text NOT NULL DEFAULT '',
    repository_ref text NOT NULL DEFAULT '',
    service_ref text NOT NULL DEFAULT '',
    branch_rules_ref text NOT NULL DEFAULT '',
    release_policy_ref text NOT NULL DEFAULT '',
    release_line_ref text NOT NULL DEFAULT '',
    provider_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    agent_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    runtime_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    initial_risk_class text NOT NULL,
    effective_risk_class text NOT NULL,
    status text NOT NULL,
    explanation text NOT NULL DEFAULT '',
    required_gates jsonb NOT NULL DEFAULT '[]'::jsonb,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_risk_assessments_target_type_chk
        CHECK (target_type IN ('transition', 'pull_request', 'release_candidate', 'runtime_job', 'policy_change', 'document', 'merge', 'postdeploy', 'rollback')),
    CONSTRAINT governance_manager_risk_assessments_target_ref_chk CHECK (target_ref <> ''),
    CONSTRAINT governance_manager_risk_assessments_provider_context_chk CHECK (jsonb_typeof(provider_context) = 'object'),
    CONSTRAINT governance_manager_risk_assessments_agent_context_chk CHECK (jsonb_typeof(agent_context) = 'object'),
    CONSTRAINT governance_manager_risk_assessments_runtime_context_chk CHECK (jsonb_typeof(runtime_context) = 'object'),
    CONSTRAINT governance_manager_risk_assessments_initial_risk_chk CHECK (initial_risk_class IN ('R0', 'R1', 'R2', 'R3')),
    CONSTRAINT governance_manager_risk_assessments_effective_risk_chk CHECK (effective_risk_class IN ('R0', 'R1', 'R2', 'R3')),
    CONSTRAINT governance_manager_risk_assessments_status_chk
        CHECK (status IN ('draft', 'active', 'superseded', 'closed')),
    CONSTRAINT governance_manager_risk_assessments_required_gates_chk CHECK (jsonb_typeof(required_gates) = 'array'),
    CONSTRAINT governance_manager_risk_assessments_version_chk CHECK (version > 0)
);

CREATE INDEX governance_manager_risk_assessments_target_idx
    ON governance_manager_risk_assessments (target_type, target_ref, status);

CREATE INDEX governance_manager_risk_assessments_project_risk_idx
    ON governance_manager_risk_assessments (project_ref, effective_risk_class, status, updated_at DESC, id);

CREATE TABLE governance_manager_risk_factors (
    id uuid PRIMARY KEY,
    risk_assessment_id uuid NOT NULL REFERENCES governance_manager_risk_assessments(id),
    source_type text NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    risk_class text NOT NULL,
    summary text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_risk_factors_source_type_chk
        CHECK (source_type IN ('policy', 'changed_file', 'service', 'api', 'database', 'secret', 'release', 'runtime', 'review_signal', 'human_decision')),
    CONSTRAINT governance_manager_risk_factors_risk_class_chk CHECK (risk_class IN ('R0', 'R1', 'R2', 'R3'))
);

CREATE INDEX governance_manager_risk_factors_assessment_source_idx
    ON governance_manager_risk_factors (risk_assessment_id, source_type, created_at, id);

CREATE TABLE governance_manager_review_signals (
    id uuid PRIMARY KEY,
    risk_assessment_id uuid REFERENCES governance_manager_risk_assessments(id),
    target_type text NOT NULL,
    target_ref text NOT NULL,
    role_kind text NOT NULL,
    author_ref text NOT NULL,
    outcome text NOT NULL,
    severity text NOT NULL,
    confidence text NOT NULL DEFAULT '',
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    summary text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_review_signals_target_type_chk
        CHECK (target_type IN ('transition', 'pull_request', 'release_candidate', 'runtime_job', 'policy_change', 'document', 'merge', 'postdeploy', 'rollback')),
    CONSTRAINT governance_manager_review_signals_target_ref_chk CHECK (target_ref <> ''),
    CONSTRAINT governance_manager_review_signals_role_kind_chk
        CHECK (role_kind IN ('reviewer', 'qa', 'lexical_gatekeeper', 'risk_gatekeeper', 'sre', 'security', 'owner', 'custom')),
    CONSTRAINT governance_manager_review_signals_author_ref_chk CHECK (author_ref <> ''),
    CONSTRAINT governance_manager_review_signals_outcome_chk
        CHECK (outcome IN ('pass', 'pass_with_notes', 'block', 'request_changes', 'raise_risk', 'informational')),
    CONSTRAINT governance_manager_review_signals_severity_chk CHECK (severity IN ('info', 'warning', 'blocking', 'critical')),
    CONSTRAINT governance_manager_review_signals_confidence_chk CHECK (confidence IN ('', 'low', 'medium', 'high')),
    CONSTRAINT governance_manager_review_signals_evidence_refs_chk CHECK (jsonb_typeof(evidence_refs) = 'array')
);

CREATE INDEX governance_manager_review_signals_target_created_idx
    ON governance_manager_review_signals (target_type, target_ref, created_at DESC, id);

CREATE INDEX governance_manager_review_signals_assessment_idx
    ON governance_manager_review_signals (risk_assessment_id, created_at DESC, id)
    WHERE risk_assessment_id IS NOT NULL;

CREATE TABLE governance_manager_gate_requests (
    id uuid PRIMARY KEY,
    risk_assessment_id uuid REFERENCES governance_manager_risk_assessments(id),
    gate_policy_id uuid REFERENCES governance_manager_gate_policies(id),
    target_type text NOT NULL,
    target_ref text NOT NULL,
    interaction_delivery_ref jsonb NOT NULL DEFAULT '{}'::jsonb,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    evidence_summary text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_gate_requests_target_type_chk
        CHECK (target_type IN ('transition', 'pull_request', 'release_candidate', 'runtime_job', 'policy_change', 'document', 'merge', 'postdeploy', 'rollback')),
    CONSTRAINT governance_manager_gate_requests_target_ref_chk CHECK (target_ref <> ''),
    CONSTRAINT governance_manager_gate_requests_interaction_ref_chk CHECK (jsonb_typeof(interaction_delivery_ref) = 'object'),
    CONSTRAINT governance_manager_gate_requests_evidence_refs_chk CHECK (jsonb_typeof(evidence_refs) = 'array'),
    CONSTRAINT governance_manager_gate_requests_status_chk
        CHECK (status IN ('requested', 'delivering', 'awaiting_decision', 'resolved', 'expired', 'cancelled')),
    CONSTRAINT governance_manager_gate_requests_version_chk CHECK (version > 0)
);

CREATE INDEX governance_manager_gate_requests_status_idx
    ON governance_manager_gate_requests (status, updated_at DESC, id);

CREATE INDEX governance_manager_gate_requests_target_idx
    ON governance_manager_gate_requests (target_type, target_ref, status, updated_at DESC, id);

CREATE TABLE governance_manager_gate_decisions (
    id uuid PRIMARY KEY,
    gate_request_id uuid NOT NULL REFERENCES governance_manager_gate_requests(id),
    decision_actor_ref text NOT NULL,
    decision_policy_ref text NOT NULL DEFAULT '',
    outcome text NOT NULL,
    reason text NOT NULL DEFAULT '',
    conditions_summary text NOT NULL DEFAULT '',
    source_ref text NOT NULL DEFAULT '',
    decided_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_gate_decisions_actor_ref_chk CHECK (decision_actor_ref <> ''),
    CONSTRAINT governance_manager_gate_decisions_outcome_chk
        CHECK (outcome IN ('approve', 'approve_with_conditions', 'revise', 'reject', 'hold', 'rollback', 'escalate')),
    UNIQUE (gate_request_id)
);

CREATE INDEX governance_manager_gate_decisions_outcome_idx
    ON governance_manager_gate_decisions (outcome, decided_at DESC, id);

CREATE TABLE governance_manager_release_decision_packages (
    id uuid PRIMARY KEY,
    release_candidate_ref text NOT NULL,
    project_ref text NOT NULL,
    repository_ref text NOT NULL DEFAULT '',
    service_ref text NOT NULL DEFAULT '',
    branch_rules_ref text NOT NULL DEFAULT '',
    release_policy_ref text NOT NULL DEFAULT '',
    release_line_ref text NOT NULL DEFAULT '',
    repository_refs text[] NOT NULL DEFAULT '{}',
    risk_assessment_id uuid REFERENCES governance_manager_risk_assessments(id),
    provider_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    runtime_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    agent_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    review_signal_ids uuid[] NOT NULL DEFAULT '{}',
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    known_limitations_summary text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_release_packages_candidate_chk CHECK (release_candidate_ref <> ''),
    CONSTRAINT governance_manager_release_packages_project_ref_chk CHECK (project_ref <> ''),
    CONSTRAINT governance_manager_release_packages_provider_refs_chk CHECK (jsonb_typeof(provider_refs) = 'array'),
    CONSTRAINT governance_manager_release_packages_runtime_refs_chk CHECK (jsonb_typeof(runtime_refs) = 'array'),
    CONSTRAINT governance_manager_release_packages_agent_context_chk CHECK (jsonb_typeof(agent_context) = 'object'),
    CONSTRAINT governance_manager_release_packages_evidence_refs_chk CHECK (jsonb_typeof(evidence_refs) = 'array'),
    CONSTRAINT governance_manager_release_packages_status_chk
        CHECK (status IN ('draft', 'ready', 'decision_requested', 'closed')),
    CONSTRAINT governance_manager_release_packages_version_chk CHECK (version > 0)
);

CREATE INDEX governance_manager_release_packages_candidate_idx
    ON governance_manager_release_decision_packages (release_candidate_ref, status, updated_at DESC, id);

CREATE INDEX governance_manager_release_packages_project_idx
    ON governance_manager_release_decision_packages (project_ref, status, updated_at DESC, id);

CREATE TABLE governance_manager_release_decisions (
    id uuid PRIMARY KEY,
    release_decision_package_id uuid NOT NULL REFERENCES governance_manager_release_decision_packages(id),
    gate_decision_id uuid REFERENCES governance_manager_gate_decisions(id),
    outcome text NOT NULL,
    decision_actor_ref text NOT NULL,
    decision_policy_ref text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT '',
    conditions_summary text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    decided_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_release_decisions_outcome_chk
        CHECK (outcome IN ('go', 'go_with_conditions', 'no_go', 'hold', 'rollback', 'follow_up_required')),
    CONSTRAINT governance_manager_release_decisions_actor_ref_chk CHECK (decision_actor_ref <> ''),
    CONSTRAINT governance_manager_release_decisions_status_chk CHECK (status IN ('requested', 'resolved', 'cancelled')),
    CONSTRAINT governance_manager_release_decisions_version_chk CHECK (version > 0)
);

CREATE INDEX governance_manager_release_decisions_package_idx
    ON governance_manager_release_decisions (release_decision_package_id, status, decided_at DESC, id);

CREATE TABLE governance_manager_release_safety_states (
    id uuid PRIMARY KEY,
    release_decision_package_id uuid NOT NULL REFERENCES governance_manager_release_decision_packages(id),
    current_state text NOT NULL,
    runtime_job_ref text NOT NULL DEFAULT '',
    blocking_signal_count integer NOT NULL DEFAULT 0,
    last_state_reason text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT governance_manager_release_safety_states_state_chk
        CHECK (current_state IN ('release_candidate', 'awaiting_release_gate', 'deploying', 'postdeploy_observation', 'stable', 'hold', 'rollback', 'follow_up_required')),
    CONSTRAINT governance_manager_release_safety_states_blocking_count_chk CHECK (blocking_signal_count >= 0),
    CONSTRAINT governance_manager_release_safety_states_version_chk CHECK (version > 0)
);

CREATE INDEX governance_manager_release_safety_states_package_state_idx
    ON governance_manager_release_safety_states (release_decision_package_id, current_state);

CREATE TABLE governance_manager_blocking_signals (
    id uuid PRIMARY KEY,
    target_type text NOT NULL,
    target_ref text NOT NULL,
    source_type text NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    severity text NOT NULL,
    summary text NOT NULL DEFAULT '',
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    resolved_at timestamptz,
    CONSTRAINT governance_manager_blocking_signals_target_type_chk
        CHECK (target_type IN ('transition', 'pull_request', 'release_candidate', 'runtime_job', 'policy_change', 'document', 'merge', 'postdeploy', 'rollback')),
    CONSTRAINT governance_manager_blocking_signals_target_ref_chk CHECK (target_ref <> ''),
    CONSTRAINT governance_manager_blocking_signals_source_type_chk
        CHECK (source_type IN ('acceptance', 'review_signal', 'runtime', 'provider', 'interaction', 'human', 'monitoring')),
    CONSTRAINT governance_manager_blocking_signals_severity_chk CHECK (severity IN ('warning', 'blocking', 'critical')),
    CONSTRAINT governance_manager_blocking_signals_status_chk CHECK (status IN ('active', 'resolved', 'dismissed')),
    CONSTRAINT governance_manager_blocking_signals_version_chk CHECK (version > 0)
);

CREATE INDEX governance_manager_blocking_signals_status_idx
    ON governance_manager_blocking_signals (status, severity, created_at DESC, id);

CREATE TABLE governance_manager_command_results (
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
    CONSTRAINT governance_manager_command_results_actor_type_chk CHECK (actor_type <> ''),
    CONSTRAINT governance_manager_command_results_actor_id_chk CHECK (actor_id <> ''),
    CONSTRAINT governance_manager_command_results_operation_chk CHECK (operation <> ''),
    CONSTRAINT governance_manager_command_results_aggregate_type_chk
        CHECK (aggregate_type IN (
            'risk_profile',
            'risk_profile_version',
            'risk_assessment',
            'review_signal',
            'gate_request',
            'gate_decision',
            'release_decision_package',
            'release_decision',
            'release_safety_state',
            'blocking_signal'
        )),
    CONSTRAINT governance_manager_command_results_payload_chk CHECK (jsonb_typeof(result_payload) = 'object')
);

CREATE UNIQUE INDEX governance_manager_command_results_command_uidx
    ON governance_manager_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX governance_manager_command_results_idempotency_uidx
    ON governance_manager_command_results (operation, actor_type, actor_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE TABLE governance_manager_outbox_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    failed_permanently_at timestamptz,
    failure_kind text NOT NULL DEFAULT '',
    last_error text NOT NULL DEFAULT '',
    CONSTRAINT governance_manager_outbox_events_type_chk CHECK (event_type LIKE 'governance.%'),
    CONSTRAINT governance_manager_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT governance_manager_outbox_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT governance_manager_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT governance_manager_outbox_events_attempt_count_chk CHECK (attempt_count >= 0)
);

CREATE INDEX governance_manager_outbox_events_unpublished_idx
    ON governance_manager_outbox_events (published_at, occurred_at, id)
    WHERE published_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS governance_manager_outbox_events;
DROP TABLE IF EXISTS governance_manager_command_results;
DROP TABLE IF EXISTS governance_manager_blocking_signals;
DROP TABLE IF EXISTS governance_manager_release_safety_states;
DROP TABLE IF EXISTS governance_manager_release_decisions;
DROP TABLE IF EXISTS governance_manager_release_decision_packages;
DROP TABLE IF EXISTS governance_manager_gate_decisions;
DROP TABLE IF EXISTS governance_manager_gate_requests;
DROP TABLE IF EXISTS governance_manager_review_signals;
DROP TABLE IF EXISTS governance_manager_risk_factors;
DROP TABLE IF EXISTS governance_manager_risk_assessments;
DROP TABLE IF EXISTS governance_manager_risk_rules;
DROP TABLE IF EXISTS governance_manager_gate_policies;
ALTER TABLE governance_manager_risk_profiles
    DROP CONSTRAINT IF EXISTS governance_manager_risk_profiles_active_version_fk;
DROP TABLE IF EXISTS governance_manager_risk_profile_versions;
DROP TABLE IF EXISTS governance_manager_risk_profiles;
