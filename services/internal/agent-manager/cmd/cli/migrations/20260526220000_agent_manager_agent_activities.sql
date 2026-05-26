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
            'activity'
        ));

CREATE TABLE agent_manager_agent_activities (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES agent_manager_sessions(id),
    run_id uuid REFERENCES agent_manager_runs(id),
    turn_id text NOT NULL DEFAULT '',
    tool_use_id text NOT NULL DEFAULT '',
    activity_kind text NOT NULL,
    tool_name text NOT NULL DEFAULT '',
    tool_category text NOT NULL DEFAULT '',
    status text NOT NULL,
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    duration_ms bigint,
    safe_summary text NOT NULL DEFAULT '',
    payload_digest text NOT NULL DEFAULT '',
    bounded_error text NOT NULL DEFAULT '',
    safe_refs_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    safe_details_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    correlation_id text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_agent_activities_kind_chk
        CHECK (activity_kind IN (
            'lifecycle',
            'tool_use',
            'tool_result',
            'permission',
            'provider_signal',
            'runtime_signal',
            'checkpoint',
            'other'
        )),
    CONSTRAINT agent_manager_agent_activities_status_chk
        CHECK (status IN (
            'planned',
            'started',
            'succeeded',
            'failed',
            'denied',
            'waiting',
            'cancelled',
            'skipped'
        )),
    CONSTRAINT agent_manager_agent_activities_time_chk
        CHECK (
            finished_at IS NULL
            OR finished_at >= started_at
        ),
    CONSTRAINT agent_manager_agent_activities_duration_chk
        CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT agent_manager_agent_activities_token_chk
        CHECK (
            char_length(turn_id) <= 256
            AND char_length(tool_use_id) <= 256
            AND char_length(correlation_id) <= 256
            AND turn_id ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND tool_use_id ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND correlation_id ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND lower(turn_id || ' ' || tool_use_id || ' ' || correlation_id) !~ '(raw_tool_input|raw_tool_response|tool_input|tool_response|raw_provider_payload|provider_payload|workspace_path|workspace_file|workspace_files|/workspace|/home/|kubeconfig|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_agent_activities_tool_chk
        CHECK (
            char_length(tool_name) <= 128
            AND char_length(tool_category) <= 128
            AND tool_name ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND tool_category ~ '^[A-Za-z0-9._:/#@+=,-]*$'
            AND lower(tool_name || ' ' || tool_category) !~ '(raw_tool_input|raw_tool_response|tool_input|tool_response|raw_provider_payload|provider_payload|workspace_path|workspace_file|workspace_files|/workspace|/home/|kubeconfig|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_agent_activities_tool_required_chk
        CHECK (
            activity_kind NOT IN ('tool_use', 'tool_result', 'permission')
            OR tool_name <> ''
            OR tool_category <> ''
        ),
    CONSTRAINT agent_manager_agent_activities_text_chk
        CHECK (
            char_length(safe_summary) <= 1000
            AND char_length(bounded_error) <= 2000
            AND safe_summary !~ '[[:cntrl:]]'
            AND bounded_error !~ '[[:cntrl:]]'
            AND lower(safe_summary || ' ' || bounded_error) !~ '(raw_tool_input|raw_tool_response|tool_input|tool_response|raw_provider_payload|provider_payload|workspace_path|workspace_file|workspace_files|/workspace|/home/|kubeconfig|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_agent_activities_digest_chk
        CHECK (payload_digest = '' OR payload_digest ~ '^sha256:[A-Fa-f0-9]{64}$'),
    CONSTRAINT agent_manager_agent_activities_json_chk
        CHECK (
            jsonb_typeof(safe_refs_json) = 'object'
            AND jsonb_typeof(safe_details_json) = 'object'
            AND char_length(safe_refs_json::text) <= 8192
            AND char_length(safe_details_json::text) <= 8192
            AND lower(safe_refs_json::text || ' ' || safe_details_json::text) !~ '(raw_tool_input|raw_tool_response|tool_input|tool_response|raw_provider_payload|provider_payload|workspace_path|workspace_file|workspace_files|/workspace|/home/|kubeconfig|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_agent_activities_idempotency_chk
        CHECK (
            idempotency_key <> ''
            AND char_length(idempotency_key) <= 512
            AND idempotency_key ~ '^[A-Za-z0-9._:/#@+=,-]+$'
            AND lower(idempotency_key) !~ '(raw_tool_input|raw_tool_response|tool_input|tool_response|raw_provider_payload|provider_payload|workspace_path|workspace_file|workspace_files|/workspace|/home/|kubeconfig|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia)'
        ),
    CONSTRAINT agent_manager_agent_activities_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_agent_activities_session_started_idx
    ON agent_manager_agent_activities (session_id, started_at DESC, id DESC);

CREATE INDEX agent_manager_agent_activities_run_started_idx
    ON agent_manager_agent_activities (run_id, started_at DESC, id DESC)
    WHERE run_id IS NOT NULL;

CREATE INDEX agent_manager_agent_activities_session_kind_started_idx
    ON agent_manager_agent_activities (session_id, activity_kind, started_at DESC, id DESC);

CREATE INDEX agent_manager_agent_activities_run_status_started_idx
    ON agent_manager_agent_activities (run_id, status, started_at DESC, id DESC)
    WHERE run_id IS NOT NULL;

CREATE INDEX agent_manager_agent_activities_tool_use_idx
    ON agent_manager_agent_activities (tool_use_id, started_at DESC, id DESC)
    WHERE tool_use_id <> '';

-- +goose Down
DROP TABLE IF EXISTS agent_manager_agent_activities;

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
