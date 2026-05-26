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
            'follow_up'
        ));

CREATE TABLE agent_manager_follow_up_intents (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES agent_manager_sessions(id),
    run_id uuid REFERENCES agent_manager_runs(id),
    from_stage_id uuid REFERENCES agent_manager_stages(id),
    to_stage_id uuid REFERENCES agent_manager_stages(id),
    acceptance_result_id uuid REFERENCES agent_manager_acceptance_results(id),
    provider_work_item_ref text NOT NULL DEFAULT '',
    provider_pull_request_ref text NOT NULL DEFAULT '',
    provider_comment_ref text NOT NULL DEFAULT '',
    provider_review_signal_ref text NOT NULL DEFAULT '',
    provider_work_item_type text NOT NULL,
    provider_operation_ref text NOT NULL DEFAULT '',
    instruction_body_digest text NOT NULL DEFAULT '',
    safe_title text NOT NULL,
    safe_summary text NOT NULL DEFAULT '',
    role_hint text NOT NULL DEFAULT '',
    stage_hint text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_follow_up_intents_target_chk
        CHECK (
            provider_work_item_ref <> ''
            OR provider_pull_request_ref <> ''
            OR provider_comment_ref <> ''
            OR provider_review_signal_ref <> ''
        ),
    CONSTRAINT agent_manager_follow_up_intents_work_item_type_chk
        CHECK (provider_work_item_type ~ '^[A-Za-z][A-Za-z0-9._-]{0,63}$'),
    CONSTRAINT agent_manager_follow_up_intents_work_item_ref_chk
        CHECK (
            char_length(provider_work_item_ref) <= 512
            AND (
                provider_work_item_ref = ''
                OR (
                    provider_work_item_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
                    AND position(':' IN provider_work_item_ref) > 1
                    AND position(':' IN provider_work_item_ref) < char_length(provider_work_item_ref)
                    AND lower(provider_work_item_ref) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|large_report|report_body|raw_report|secret|token|authorization|stdout|stderr|logs|-----begin|bearer)'
                )
            )
        ),
    CONSTRAINT agent_manager_follow_up_intents_pull_request_ref_chk
        CHECK (
            char_length(provider_pull_request_ref) <= 512
            AND (
                provider_pull_request_ref = ''
                OR (
                    provider_pull_request_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
                    AND position(':' IN provider_pull_request_ref) > 1
                    AND position(':' IN provider_pull_request_ref) < char_length(provider_pull_request_ref)
                    AND lower(provider_pull_request_ref) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|large_report|report_body|raw_report|secret|token|authorization|stdout|stderr|logs|-----begin|bearer)'
                )
            )
        ),
    CONSTRAINT agent_manager_follow_up_intents_comment_ref_chk
        CHECK (
            char_length(provider_comment_ref) <= 512
            AND (
                provider_comment_ref = ''
                OR (
                    provider_comment_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
                    AND position(':' IN provider_comment_ref) > 1
                    AND position(':' IN provider_comment_ref) < char_length(provider_comment_ref)
                    AND lower(provider_comment_ref) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|large_report|report_body|raw_report|secret|token|authorization|stdout|stderr|logs|-----begin|bearer)'
                )
            )
        ),
    CONSTRAINT agent_manager_follow_up_intents_review_signal_ref_chk
        CHECK (
            char_length(provider_review_signal_ref) <= 512
            AND (
                provider_review_signal_ref = ''
                OR (
                    provider_review_signal_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
                    AND position(':' IN provider_review_signal_ref) > 1
                    AND position(':' IN provider_review_signal_ref) < char_length(provider_review_signal_ref)
                    AND lower(provider_review_signal_ref) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|large_report|report_body|raw_report|secret|token|authorization|stdout|stderr|logs|-----begin|bearer)'
                )
            )
        ),
    CONSTRAINT agent_manager_follow_up_intents_operation_ref_chk
        CHECK (
            char_length(provider_operation_ref) <= 512
            AND (
                provider_operation_ref = ''
                OR (
                    provider_operation_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
                    AND position(':' IN provider_operation_ref) > 1
                    AND position(':' IN provider_operation_ref) < char_length(provider_operation_ref)
                    AND lower(provider_operation_ref) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|large_report|report_body|raw_report|secret|token|authorization|stdout|stderr|logs|-----begin|bearer)'
                )
            )
        ),
    CONSTRAINT agent_manager_follow_up_intents_digest_chk
        CHECK (instruction_body_digest = '' OR instruction_body_digest ~ '^sha256:[A-Fa-f0-9]{64}$'),
    CONSTRAINT agent_manager_follow_up_intents_text_chk
        CHECK (
            char_length(safe_title) BETWEEN 1 AND 200
            AND char_length(safe_summary) <= 1000
            AND safe_title !~ '[[:cntrl:]]'
            AND safe_summary !~ '[[:cntrl:]]'
            AND lower(safe_title || ' ' || safe_summary) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|-----begin|authorization:|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_follow_up_intents_hint_chk
        CHECK (
            char_length(role_hint) <= 128
            AND char_length(stage_hint) <= 128
            AND role_hint ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND stage_hint ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND lower(role_hint || ' ' || stage_hint) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|-----begin|authorization:|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_follow_up_intents_idempotency_chk
        CHECK (idempotency_key <> '' AND char_length(idempotency_key) <= 512),
    CONSTRAINT agent_manager_follow_up_intents_status_chk
        CHECK (status IN ('planned', 'requested', 'created', 'failed', 'cancelled')),
    CONSTRAINT agent_manager_follow_up_intents_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_follow_up_intents_session_status_idx
    ON agent_manager_follow_up_intents (session_id, status, updated_at DESC, id);

CREATE INDEX agent_manager_follow_up_intents_run_status_idx
    ON agent_manager_follow_up_intents (run_id, status, updated_at DESC, id)
    WHERE run_id IS NOT NULL;

CREATE INDEX agent_manager_follow_up_intents_acceptance_idx
    ON agent_manager_follow_up_intents (acceptance_result_id)
    WHERE acceptance_result_id IS NOT NULL;

CREATE INDEX agent_manager_follow_up_intents_provider_work_item_idx
    ON agent_manager_follow_up_intents (provider_work_item_ref, updated_at DESC, id)
    WHERE provider_work_item_ref <> '';

-- +goose Down
DROP TABLE IF EXISTS agent_manager_follow_up_intents;

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
            'acceptance'
        ));
