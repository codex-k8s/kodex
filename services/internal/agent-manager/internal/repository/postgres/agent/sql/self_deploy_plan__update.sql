-- name: self_deploy_plan__update :exec
UPDATE agent_manager_self_deploy_plans
SET
    governance_risk_assessment_ref = @governance_risk_assessment_ref,
    governance_gate_request_ref = @governance_gate_request_ref,
    governance_gate_decision_ref = @governance_gate_decision_ref,
    governance_release_decision_package_ref = @governance_release_decision_package_ref,
    governance_release_decision_ref = @governance_release_decision_ref,
    governance_risk_profile_ref = @governance_risk_profile_ref,
    governance_gate_policy_ref = @governance_gate_policy_ref,
    governance_release_policy_ref = @governance_release_policy_ref,
    status = @status,
    runtime_build_jobs = @runtime_build_jobs,
    runtime_build_status = @runtime_build_status,
    runtime_build_plan_fingerprint = @runtime_build_plan_fingerprint,
    runtime_build_error_code = @runtime_build_error_code,
    runtime_build_summary = @runtime_build_summary,
    runtime_build_contexts = @runtime_build_contexts,
    runtime_deploy_jobs = @runtime_deploy_jobs,
    runtime_deploy_status = @runtime_deploy_status,
    runtime_deploy_plan_fingerprint = @runtime_deploy_plan_fingerprint,
    runtime_deploy_error_code = @runtime_deploy_error_code,
    runtime_deploy_summary = @runtime_deploy_summary,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
