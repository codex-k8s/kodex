-- +goose Up
ALTER TABLE agent_manager_self_deploy_plans
    ADD COLUMN runtime_build_jobs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN runtime_build_status text NOT NULL DEFAULT 'not_requested',
    ADD COLUMN runtime_build_plan_fingerprint text NOT NULL DEFAULT '',
    ADD COLUMN runtime_build_error_code text NOT NULL DEFAULT '',
    ADD COLUMN runtime_build_summary text NOT NULL DEFAULT '';

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_runtime_build_chk
    CHECK (
        runtime_build_status IN ('not_requested', 'blocked', 'requested', 'failed')
        AND (runtime_build_plan_fingerprint = '' OR runtime_build_plan_fingerprint ~ '^sha256:[A-Fa-f0-9]{64}$')
        AND char_length(runtime_build_error_code) <= 128
        AND char_length(runtime_build_summary) <= 1000
        AND runtime_build_error_code !~ '[[:cntrl:]]'
        AND runtime_build_summary !~ '[[:cntrl:]]'
        AND lower(runtime_build_error_code || ' ' || runtime_build_summary) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_runtime_build_jobs_chk
    CHECK (
        jsonb_typeof(runtime_build_jobs) = 'array'
        AND jsonb_array_length(runtime_build_jobs) <= 100
        AND char_length(runtime_build_jobs::text) <= 65536
        AND lower(runtime_build_jobs::text) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret_value|token_value|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

-- +goose Down
ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_build_jobs_chk;

ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_build_chk;

ALTER TABLE agent_manager_self_deploy_plans
    DROP COLUMN IF EXISTS runtime_build_summary,
    DROP COLUMN IF EXISTS runtime_build_error_code,
    DROP COLUMN IF EXISTS runtime_build_plan_fingerprint,
    DROP COLUMN IF EXISTS runtime_build_status,
    DROP COLUMN IF EXISTS runtime_build_jobs;
