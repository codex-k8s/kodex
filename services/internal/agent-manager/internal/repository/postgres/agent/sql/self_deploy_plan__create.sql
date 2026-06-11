-- name: self_deploy_plan__create :exec
INSERT INTO agent_manager_self_deploy_plans (
    id, scope_type, scope_ref, project_ref, repository_ref, provider_signal_ref,
    provider_slug, repository_full_name, provider_repository_id,
    source_ref, merge_commit_sha, services_yaml_ref, services_yaml_digest,
    affected_service_keys, path_categories, expected_runtime_job_types,
    governance_risk_assessment_ref, governance_gate_request_ref,
    governance_gate_decision_ref, governance_release_decision_package_ref,
    governance_release_decision_ref, governance_risk_profile_ref,
    governance_gate_policy_ref, governance_release_policy_ref,
    safe_summary, plan_fingerprint, idempotency_key, status,
    runtime_build_jobs, runtime_build_status, runtime_build_plan_fingerprint,
    runtime_build_error_code, runtime_build_summary,
    runtime_build_contexts, runtime_deploy_jobs, runtime_deploy_status,
    runtime_deploy_plan_fingerprint, runtime_deploy_error_code, runtime_deploy_summary,
    version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @project_ref, @repository_ref, @provider_signal_ref,
    @provider_slug, @repository_full_name, @provider_repository_id,
    @source_ref, @merge_commit_sha, @services_yaml_ref, @services_yaml_digest,
    @affected_service_keys, @path_categories, @expected_runtime_job_types,
    @governance_risk_assessment_ref, @governance_gate_request_ref,
    @governance_gate_decision_ref, @governance_release_decision_package_ref,
    @governance_release_decision_ref, @governance_risk_profile_ref,
    @governance_gate_policy_ref, @governance_release_policy_ref,
    @safe_summary, @plan_fingerprint, @idempotency_key, @status,
    @runtime_build_jobs, @runtime_build_status, @runtime_build_plan_fingerprint,
    @runtime_build_error_code, @runtime_build_summary,
    @runtime_build_contexts, @runtime_deploy_jobs, @runtime_deploy_status,
    @runtime_deploy_plan_fingerprint, @runtime_deploy_error_code, @runtime_deploy_summary,
    @version, @created_at, @updated_at
);
