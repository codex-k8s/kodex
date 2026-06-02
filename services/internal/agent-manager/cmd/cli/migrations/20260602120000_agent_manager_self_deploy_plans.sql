-- +goose Up
ALTER TABLE agent_manager_command_results
    DROP CONSTRAINT agent_manager_command_results_aggregate_type_chk;

ALTER TABLE agent_manager_command_results
    ADD CONSTRAINT agent_manager_command_results_aggregate_type_chk
        CHECK (aggregate_type IN (
            'flow',
            'flow_version',
            'role_profile',
            'prompt_template',
            'prompt_template_version',
            'session',
            'run',
            'session_state_snapshot',
            'acceptance',
            'follow_up',
            'activity',
            'human_gate',
            'self_deploy_plan'
        ));

CREATE TABLE agent_manager_self_deploy_plans (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    project_ref text NOT NULL,
    repository_ref text NOT NULL,
    provider_signal_ref text NOT NULL DEFAULT '',
    source_ref text NOT NULL,
    merge_commit_sha text NOT NULL,
    services_yaml_ref text NOT NULL DEFAULT '',
    services_yaml_digest text NOT NULL,
    affected_service_keys jsonb NOT NULL DEFAULT '[]'::jsonb,
    path_categories jsonb NOT NULL DEFAULT '[]'::jsonb,
    expected_runtime_job_types jsonb NOT NULL DEFAULT '[]'::jsonb,
    governance_risk_assessment_ref text NOT NULL DEFAULT '',
    governance_gate_request_ref text NOT NULL DEFAULT '',
    governance_gate_decision_ref text NOT NULL DEFAULT '',
    governance_release_decision_package_ref text NOT NULL DEFAULT '',
    governance_release_decision_ref text NOT NULL DEFAULT '',
    governance_risk_profile_ref text NOT NULL DEFAULT '',
    governance_gate_policy_ref text NOT NULL DEFAULT '',
    governance_release_policy_ref text NOT NULL DEFAULT '',
    safe_summary text NOT NULL DEFAULT '',
    plan_fingerprint text NOT NULL,
    idempotency_key text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_self_deploy_plans_scope_chk
        CHECK (
            scope_type IN ('platform', 'organization', 'project', 'repository')
            AND char_length(scope_ref) BETWEEN 1 AND 512
            AND scope_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
        ),
    CONSTRAINT agent_manager_self_deploy_plans_refs_chk
        CHECK (
            char_length(project_ref) BETWEEN 1 AND 512
            AND char_length(repository_ref) BETWEEN 1 AND 512
            AND char_length(provider_signal_ref) <= 512
            AND char_length(source_ref) BETWEEN 1 AND 512
            AND char_length(services_yaml_ref) <= 512
            AND project_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND repository_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND (provider_signal_ref = '' OR provider_signal_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND source_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND (services_yaml_ref = '' OR services_yaml_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND lower(
                project_ref || ' ' || repository_ref || ' ' || provider_signal_ref || ' ' ||
                source_ref || ' ' || services_yaml_ref
            ) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
        ),
    CONSTRAINT agent_manager_self_deploy_plans_commit_chk
        CHECK (merge_commit_sha ~ '^[A-Fa-f0-9]{40}([A-Fa-f0-9]{24})?$'),
    CONSTRAINT agent_manager_self_deploy_plans_digest_chk
        CHECK (
            services_yaml_digest ~ '^sha256:[A-Fa-f0-9]{64}$'
            AND plan_fingerprint ~ '^sha256:[A-Fa-f0-9]{64}$'
        ),
    CONSTRAINT agent_manager_self_deploy_plans_arrays_chk
        CHECK (
            jsonb_typeof(affected_service_keys) = 'array'
            AND jsonb_typeof(path_categories) = 'array'
            AND jsonb_typeof(expected_runtime_job_types) = 'array'
            AND jsonb_array_length(affected_service_keys) BETWEEN 1 AND 100
            AND jsonb_array_length(path_categories) BETWEEN 1 AND 16
            AND jsonb_array_length(expected_runtime_job_types) BETWEEN 1 AND 8
        ),
    CONSTRAINT agent_manager_self_deploy_plans_governance_refs_chk
        CHECK (
            char_length(governance_risk_assessment_ref) <= 512
            AND char_length(governance_gate_request_ref) <= 512
            AND char_length(governance_gate_decision_ref) <= 512
            AND char_length(governance_release_decision_package_ref) <= 512
            AND char_length(governance_release_decision_ref) <= 512
            AND char_length(governance_risk_profile_ref) <= 512
            AND char_length(governance_gate_policy_ref) <= 512
            AND char_length(governance_release_policy_ref) <= 512
            AND (governance_risk_assessment_ref = '' OR governance_risk_assessment_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_gate_request_ref = '' OR governance_gate_request_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_gate_decision_ref = '' OR governance_gate_decision_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_release_decision_package_ref = '' OR governance_release_decision_package_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_release_decision_ref = '' OR governance_release_decision_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_risk_profile_ref = '' OR governance_risk_profile_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_gate_policy_ref = '' OR governance_gate_policy_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_release_policy_ref = '' OR governance_release_policy_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_gate_decision_ref = '' OR governance_gate_request_ref <> '')
            AND (governance_release_decision_ref = '' OR governance_release_decision_package_ref <> '')
        ),
    CONSTRAINT agent_manager_self_deploy_plans_text_chk
        CHECK (
            char_length(safe_summary) <= 1000
            AND safe_summary !~ '[[:cntrl:]]'
            AND lower(safe_summary) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
        ),
    CONSTRAINT agent_manager_self_deploy_plans_idempotency_chk
        CHECK (
            idempotency_key <> ''
            AND char_length(idempotency_key) <= 512
            AND idempotency_key ~ '^[A-Za-z0-9._:/#@+=,-]+$'
        ),
    CONSTRAINT agent_manager_self_deploy_plans_status_chk
        CHECK (status IN ('pending_approval', 'approved', 'rejected', 'cancelled', 'failed')),
    CONSTRAINT agent_manager_self_deploy_plans_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_self_deploy_plans_scope_status_idx
    ON agent_manager_self_deploy_plans (scope_type, scope_ref, status, updated_at DESC, id);

CREATE INDEX agent_manager_self_deploy_plans_project_repo_idx
    ON agent_manager_self_deploy_plans (project_ref, repository_ref, updated_at DESC, id);

CREATE INDEX agent_manager_self_deploy_plans_signal_idx
    ON agent_manager_self_deploy_plans (provider_signal_ref, updated_at DESC, id)
    WHERE provider_signal_ref <> '';

-- +goose Down
DROP TABLE IF EXISTS agent_manager_self_deploy_plans;

ALTER TABLE agent_manager_command_results
    DROP CONSTRAINT agent_manager_command_results_aggregate_type_chk;

ALTER TABLE agent_manager_command_results
    ADD CONSTRAINT agent_manager_command_results_aggregate_type_chk
        CHECK (aggregate_type IN (
            'flow',
            'flow_version',
            'role_profile',
            'prompt_template',
            'prompt_template_version',
            'session',
            'run',
            'session_state_snapshot',
            'acceptance',
            'follow_up',
            'activity',
            'human_gate'
        ));
