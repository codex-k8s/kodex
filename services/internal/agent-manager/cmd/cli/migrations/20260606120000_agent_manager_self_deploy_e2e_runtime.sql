-- +goose Up
ALTER TABLE agent_manager_self_deploy_plans
    ADD COLUMN provider_slug text NOT NULL DEFAULT '',
    ADD COLUMN repository_full_name text NOT NULL DEFAULT '',
    ADD COLUMN provider_repository_id text NOT NULL DEFAULT '',
    ADD COLUMN runtime_build_contexts jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN runtime_deploy_jobs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN runtime_deploy_status text NOT NULL DEFAULT 'not_requested',
    ADD COLUMN runtime_deploy_plan_fingerprint text NOT NULL DEFAULT '',
    ADD COLUMN runtime_deploy_error_code text NOT NULL DEFAULT '',
    ADD COLUMN runtime_deploy_summary text NOT NULL DEFAULT '';

ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_build_chk;

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_runtime_build_chk
    CHECK (
        runtime_build_status IN ('not_requested', 'preparing_context', 'blocked', 'requested', 'failed', 'succeeded')
        AND (runtime_build_plan_fingerprint = '' OR runtime_build_plan_fingerprint ~ '^sha256:[A-Fa-f0-9]{64}$')
        AND char_length(runtime_build_error_code) <= 128
        AND char_length(runtime_build_summary) <= 1000
        AND runtime_build_error_code !~ '[[:cntrl:]]'
        AND runtime_build_summary !~ '[[:cntrl:]]'
        AND lower(runtime_build_error_code || ' ' || runtime_build_summary) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_provider_identity_chk
    CHECK (
        char_length(provider_slug) <= 64
        AND char_length(repository_full_name) <= 256
        AND char_length(provider_repository_id) <= 128
        AND provider_slug !~ '[[:cntrl:]]'
        AND repository_full_name !~ '[[:cntrl:]]'
        AND provider_repository_id !~ '[[:cntrl:]]'
        AND lower(provider_slug || ' ' || repository_full_name || ' ' || provider_repository_id) !~ '(secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_runtime_build_contexts_chk
    CHECK (
        jsonb_typeof(runtime_build_contexts) = 'array'
        AND jsonb_array_length(runtime_build_contexts) <= 100
        AND char_length(runtime_build_contexts::text) <= 65536
        AND lower(runtime_build_contexts::text) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret_value|token_value|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_runtime_deploy_chk
    CHECK (
        runtime_deploy_status IN ('not_requested', 'blocked', 'requested', 'failed', 'succeeded')
        AND (runtime_deploy_plan_fingerprint = '' OR runtime_deploy_plan_fingerprint ~ '^sha256:[A-Fa-f0-9]{64}$')
        AND char_length(runtime_deploy_error_code) <= 128
        AND char_length(runtime_deploy_summary) <= 1000
        AND runtime_deploy_error_code !~ '[[:cntrl:]]'
        AND runtime_deploy_summary !~ '[[:cntrl:]]'
        AND lower(runtime_deploy_error_code || ' ' || runtime_deploy_summary) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

ALTER TABLE agent_manager_self_deploy_plans
    ADD CONSTRAINT agent_manager_self_deploy_plans_runtime_deploy_jobs_chk
    CHECK (
        jsonb_typeof(runtime_deploy_jobs) = 'array'
        AND jsonb_array_length(runtime_deploy_jobs) <= 100
        AND char_length(runtime_deploy_jobs::text) <= 65536
        AND lower(runtime_deploy_jobs::text) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret_value|token_value|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|webhook_body|full_diff|full_yaml|kubeconfig)'
    );

-- +goose Down
ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_deploy_jobs_chk;

ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_deploy_chk;

ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_build_contexts_chk;

ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_provider_identity_chk;

ALTER TABLE agent_manager_self_deploy_plans
    DROP CONSTRAINT IF EXISTS agent_manager_self_deploy_plans_runtime_build_chk;

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
    DROP COLUMN IF EXISTS runtime_deploy_summary,
    DROP COLUMN IF EXISTS runtime_deploy_error_code,
    DROP COLUMN IF EXISTS runtime_deploy_plan_fingerprint,
    DROP COLUMN IF EXISTS runtime_deploy_status,
    DROP COLUMN IF EXISTS runtime_deploy_jobs,
    DROP COLUMN IF EXISTS runtime_build_contexts,
    DROP COLUMN IF EXISTS provider_repository_id,
    DROP COLUMN IF EXISTS repository_full_name,
    DROP COLUMN IF EXISTS provider_slug;
