-- +goose Up
ALTER TABLE agent_manager_acceptance_results
    ADD COLUMN governance_risk_assessment_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_request_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_decision_package_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_risk_profile_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_policy_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_policy_ref text NOT NULL DEFAULT '';

ALTER TABLE agent_manager_acceptance_results
    ADD CONSTRAINT agent_manager_acceptance_results_governance_refs_chk
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
            AND lower(
                governance_risk_assessment_ref || ' ' || governance_gate_request_ref || ' ' ||
                governance_gate_decision_ref || ' ' || governance_release_decision_package_ref || ' ' ||
                governance_release_decision_ref || ' ' || governance_risk_profile_ref || ' ' ||
                governance_gate_policy_ref || ' ' || governance_release_policy_ref
            ) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    ADD CONSTRAINT agent_manager_acceptance_results_governance_refs_consistency_chk
        CHECK (
            (governance_gate_decision_ref = '' OR governance_gate_request_ref <> '')
            AND (governance_release_decision_ref = '' OR governance_release_decision_package_ref <> '')
        );

ALTER TABLE agent_manager_follow_up_intents
    ADD COLUMN governance_risk_assessment_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_request_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_decision_package_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_risk_profile_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_policy_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_policy_ref text NOT NULL DEFAULT '';

ALTER TABLE agent_manager_follow_up_intents
    ADD CONSTRAINT agent_manager_follow_up_intents_governance_refs_chk
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
            AND lower(
                governance_risk_assessment_ref || ' ' || governance_gate_request_ref || ' ' ||
                governance_gate_decision_ref || ' ' || governance_release_decision_package_ref || ' ' ||
                governance_release_decision_ref || ' ' || governance_risk_profile_ref || ' ' ||
                governance_gate_policy_ref || ' ' || governance_release_policy_ref
            ) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    ADD CONSTRAINT agent_manager_follow_up_intents_governance_refs_consistency_chk
        CHECK (
            (governance_gate_decision_ref = '' OR governance_gate_request_ref <> '')
            AND (governance_release_decision_ref = '' OR governance_release_decision_package_ref <> '')
        );

ALTER TABLE agent_manager_human_gate_requests
    ADD COLUMN governance_risk_assessment_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_decision_package_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_risk_profile_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_gate_policy_ref text NOT NULL DEFAULT '',
    ADD COLUMN governance_release_policy_ref text NOT NULL DEFAULT '';

ALTER TABLE agent_manager_human_gate_requests
    ADD CONSTRAINT agent_manager_human_gate_requests_governance_context_refs_chk
        CHECK (
            char_length(governance_risk_assessment_ref) <= 512
            AND char_length(governance_release_decision_package_ref) <= 512
            AND char_length(governance_release_decision_ref) <= 512
            AND char_length(governance_risk_profile_ref) <= 512
            AND char_length(governance_gate_policy_ref) <= 512
            AND char_length(governance_release_policy_ref) <= 512
            AND (governance_risk_assessment_ref = '' OR governance_risk_assessment_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_release_decision_package_ref = '' OR governance_release_decision_package_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_release_decision_ref = '' OR governance_release_decision_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_risk_profile_ref = '' OR governance_risk_profile_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_gate_policy_ref = '' OR governance_gate_policy_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND (governance_release_policy_ref = '' OR governance_release_policy_ref ~ '^[A-Za-z0-9._:/#@+=,-]+$')
            AND lower(
                governance_risk_assessment_ref || ' ' || governance_release_decision_package_ref || ' ' ||
                governance_release_decision_ref || ' ' || governance_risk_profile_ref || ' ' ||
                governance_gate_policy_ref || ' ' || governance_release_policy_ref
            ) !~ '(raw_provider_payload|provider_payload|workspace_file|workspace_files|prompt_text|prompt_template|flow_file|transcript|session_dump|large_report|report_body|raw_report|stdout|stderr|logs|secret|token|authorization|-----begin|bearer |ghp_|glpat-|xoxb-|akia|email|phone|address|pii|interaction_payload|governance_payload)'
        ),
    ADD CONSTRAINT agent_manager_human_gate_requests_governance_context_consistency_chk
        CHECK (
            (governance_decision_ref = '' OR governance_gate_request_ref <> '')
            AND (governance_release_decision_ref = '' OR governance_release_decision_package_ref <> '')
        );

-- +goose Down
ALTER TABLE agent_manager_human_gate_requests
    DROP CONSTRAINT IF EXISTS agent_manager_human_gate_requests_governance_context_consistency_chk,
    DROP CONSTRAINT IF EXISTS agent_manager_human_gate_requests_governance_context_refs_chk,
    DROP COLUMN IF EXISTS governance_release_policy_ref,
    DROP COLUMN IF EXISTS governance_gate_policy_ref,
    DROP COLUMN IF EXISTS governance_risk_profile_ref,
    DROP COLUMN IF EXISTS governance_release_decision_ref,
    DROP COLUMN IF EXISTS governance_release_decision_package_ref,
    DROP COLUMN IF EXISTS governance_risk_assessment_ref;

ALTER TABLE agent_manager_follow_up_intents
    DROP CONSTRAINT IF EXISTS agent_manager_follow_up_intents_governance_refs_consistency_chk,
    DROP CONSTRAINT IF EXISTS agent_manager_follow_up_intents_governance_refs_chk,
    DROP COLUMN IF EXISTS governance_release_policy_ref,
    DROP COLUMN IF EXISTS governance_gate_policy_ref,
    DROP COLUMN IF EXISTS governance_risk_profile_ref,
    DROP COLUMN IF EXISTS governance_release_decision_ref,
    DROP COLUMN IF EXISTS governance_release_decision_package_ref,
    DROP COLUMN IF EXISTS governance_gate_decision_ref,
    DROP COLUMN IF EXISTS governance_gate_request_ref,
    DROP COLUMN IF EXISTS governance_risk_assessment_ref;

ALTER TABLE agent_manager_acceptance_results
    DROP CONSTRAINT IF EXISTS agent_manager_acceptance_results_governance_refs_consistency_chk,
    DROP CONSTRAINT IF EXISTS agent_manager_acceptance_results_governance_refs_chk,
    DROP COLUMN IF EXISTS governance_release_policy_ref,
    DROP COLUMN IF EXISTS governance_gate_policy_ref,
    DROP COLUMN IF EXISTS governance_risk_profile_ref,
    DROP COLUMN IF EXISTS governance_release_decision_ref,
    DROP COLUMN IF EXISTS governance_release_decision_package_ref,
    DROP COLUMN IF EXISTS governance_gate_decision_ref,
    DROP COLUMN IF EXISTS governance_gate_request_ref,
    DROP COLUMN IF EXISTS governance_risk_assessment_ref;
