-- +goose Up
ALTER TABLE governance_manager_risk_assessments
    DROP CONSTRAINT IF EXISTS governance_manager_risk_assessments_target_type_chk,
    ADD CONSTRAINT governance_manager_risk_assessments_target_type_chk
        CHECK (target_type IN ('transition', 'pull_request', 'release_candidate', 'runtime_job', 'policy_change', 'document', 'merge', 'postdeploy', 'rollback', 'self_deploy_plan'));

ALTER TABLE governance_manager_gate_requests
    DROP CONSTRAINT IF EXISTS governance_manager_gate_requests_target_type_chk,
    ADD CONSTRAINT governance_manager_gate_requests_target_type_chk
        CHECK (target_type IN ('transition', 'pull_request', 'release_candidate', 'runtime_job', 'policy_change', 'document', 'merge', 'postdeploy', 'rollback', 'self_deploy_plan'));

-- +goose Down
SELECT 1;
