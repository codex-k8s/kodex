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
            'acceptance'
        ));

CREATE TABLE agent_manager_acceptance_results (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES agent_manager_sessions(id),
    run_id uuid REFERENCES agent_manager_runs(id),
    stage_id uuid REFERENCES agent_manager_stages(id),
    check_kind text NOT NULL,
    status text NOT NULL,
    target_ref text NOT NULL DEFAULT '',
    details_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_acceptance_results_kind_chk
        CHECK (check_kind IN ('artifact', 'watermark', 'policy', 'role_result', 'human_gate', 'follow_up')),
    CONSTRAINT agent_manager_acceptance_results_status_chk
        CHECK (status IN ('pending', 'passed', 'failed', 'waiting', 'skipped')),
    CONSTRAINT agent_manager_acceptance_results_target_ref_chk
        CHECK (
            char_length(target_ref) <= 512
            AND (
                target_ref = ''
                OR (
                    target_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$'
                    AND position(':' IN target_ref) > 1
                    AND position(':' IN target_ref) < char_length(target_ref)
                    AND lower(target_ref) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|large_report|report_body|raw_report|secret|token|authorization|stdout|stderr|logs|-----begin|bearer)'
                )
            )
        ),
    CONSTRAINT agent_manager_acceptance_results_details_chk CHECK (jsonb_typeof(details_json) = 'object'),
    CONSTRAINT agent_manager_acceptance_results_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_acceptance_results_session_status_idx
    ON agent_manager_acceptance_results (session_id, status, updated_at DESC, id);

CREATE INDEX agent_manager_acceptance_results_run_status_idx
    ON agent_manager_acceptance_results (run_id, status, updated_at DESC, id)
    WHERE run_id IS NOT NULL;

CREATE INDEX agent_manager_acceptance_results_stage_status_idx
    ON agent_manager_acceptance_results (stage_id, status, updated_at DESC, id)
    WHERE stage_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS agent_manager_acceptance_results;

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
            'session_state_snapshot'
        ));
