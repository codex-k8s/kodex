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
            'human_gate'
        ));

CREATE TABLE agent_manager_human_gate_requests (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES agent_manager_sessions(id),
    run_id uuid REFERENCES agent_manager_runs(id),
    stage_id uuid REFERENCES agent_manager_stages(id),
    acceptance_result_id uuid REFERENCES agent_manager_acceptance_results(id),
    provider_work_item_ref text NOT NULL DEFAULT '',
    provider_pull_request_ref text NOT NULL DEFAULT '',
    provider_comment_ref text NOT NULL DEFAULT '',
    provider_review_signal_ref text NOT NULL DEFAULT '',
    target_ref text NOT NULL DEFAULT '',
    request_kind text NOT NULL,
    reason_code text NOT NULL,
    safe_summary text NOT NULL DEFAULT '',
    interaction_request_ref text NOT NULL DEFAULT '',
    interaction_response_ref text NOT NULL DEFAULT '',
    governance_gate_request_ref text NOT NULL DEFAULT '',
    governance_decision_ref text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    status text NOT NULL,
    outcome text NOT NULL,
    version bigint NOT NULL,
    resolved_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_human_gate_requests_code_chk
        CHECK (
            char_length(request_kind) BETWEEN 1 AND 128
            AND char_length(reason_code) BETWEEN 1 AND 128
            AND request_kind ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND reason_code ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND lower(request_kind || ' ' || reason_code) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    CONSTRAINT agent_manager_human_gate_requests_text_chk
        CHECK (
            char_length(safe_summary) <= 1000
            AND safe_summary !~ '[[:cntrl:]]'
            AND lower(safe_summary) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    CONSTRAINT agent_manager_human_gate_requests_refs_chk
        CHECK (
            char_length(provider_work_item_ref) <= 512
            AND char_length(provider_pull_request_ref) <= 512
            AND char_length(provider_comment_ref) <= 512
            AND char_length(provider_review_signal_ref) <= 512
            AND char_length(target_ref) <= 512
            AND char_length(interaction_request_ref) <= 512
            AND char_length(interaction_response_ref) <= 512
            AND char_length(governance_gate_request_ref) <= 512
            AND char_length(governance_decision_ref) <= 512
            AND (provider_work_item_ref = '' OR provider_work_item_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (provider_pull_request_ref = '' OR provider_pull_request_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (provider_comment_ref = '' OR provider_comment_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (provider_review_signal_ref = '' OR provider_review_signal_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (target_ref = '' OR target_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (interaction_request_ref = '' OR interaction_request_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (interaction_response_ref = '' OR interaction_response_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_gate_request_ref = '' OR governance_gate_request_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_decision_ref = '' OR governance_decision_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND lower(
                provider_work_item_ref || ' ' || provider_pull_request_ref || ' ' ||
                provider_comment_ref || ' ' || provider_review_signal_ref || ' ' ||
                target_ref || ' ' || interaction_request_ref || ' ' ||
                interaction_response_ref || ' ' || governance_gate_request_ref || ' ' ||
                governance_decision_ref
            ) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    CONSTRAINT agent_manager_human_gate_requests_idempotency_chk
        CHECK (
            idempotency_key <> ''
            AND char_length(idempotency_key) <= 512
            AND idempotency_key ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND lower(idempotency_key) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    CONSTRAINT agent_manager_human_gate_requests_status_chk
        CHECK (status IN ('requested', 'waiting', 'resolved', 'failed', 'cancelled')),
    CONSTRAINT agent_manager_human_gate_requests_outcome_chk
        CHECK (outcome IN ('none', 'approve', 'reject', 'request_changes', 'answer')),
    CONSTRAINT agent_manager_human_gate_requests_terminal_chk
        CHECK (
            (
                status = 'resolved'
                AND outcome <> 'none'
                AND resolved_at IS NOT NULL
                AND (interaction_response_ref <> '' OR governance_decision_ref <> '')
            )
            OR (
                status <> 'resolved'
                AND outcome = 'none'
                AND resolved_at IS NULL
            )
        ),
    CONSTRAINT agent_manager_human_gate_requests_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_human_gate_requests_session_status_idx
    ON agent_manager_human_gate_requests (session_id, status, updated_at DESC, id);

CREATE INDEX agent_manager_human_gate_requests_run_status_idx
    ON agent_manager_human_gate_requests (run_id, status, updated_at DESC, id)
    WHERE run_id IS NOT NULL;

CREATE INDEX agent_manager_human_gate_requests_stage_status_idx
    ON agent_manager_human_gate_requests (stage_id, status, updated_at DESC, id)
    WHERE stage_id IS NOT NULL;

CREATE INDEX agent_manager_human_gate_requests_acceptance_idx
    ON agent_manager_human_gate_requests (acceptance_result_id)
    WHERE acceptance_result_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS agent_manager_human_gate_requests;

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
            'activity'
        ));
