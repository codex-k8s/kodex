-- name: self_deploy_plan__list :many
SELECT
    id,
    scope_type,
    scope_ref,
    project_ref,
    repository_ref,
    provider_signal_ref,
    source_ref,
    merge_commit_sha,
    services_yaml_ref,
    services_yaml_digest,
    affected_service_keys,
    path_categories,
    expected_runtime_job_types,
    governance_risk_assessment_ref,
    governance_gate_request_ref,
    governance_gate_decision_ref,
    governance_release_decision_package_ref,
    governance_release_decision_ref,
    governance_risk_profile_ref,
    governance_gate_policy_ref,
    governance_release_policy_ref,
    safe_summary,
    plan_fingerprint,
    idempotency_key,
    status,
    runtime_build_jobs,
    runtime_build_status,
    runtime_build_plan_fingerprint,
    runtime_build_error_code,
    runtime_build_summary,
    version,
    created_at,
    updated_at
FROM agent_manager_self_deploy_plans
WHERE (@scope_type::text IS NULL OR scope_type = @scope_type)
  AND (@scope_ref::text IS NULL OR scope_ref = @scope_ref)
  AND (@project_ref::text IS NULL OR project_ref = @project_ref)
  AND (@repository_ref::text IS NULL OR repository_ref = @repository_ref)
  AND (@provider_signal_ref::text IS NULL OR provider_signal_ref = @provider_signal_ref)
  AND (@status::text IS NULL OR status = @status)
ORDER BY updated_at DESC, id DESC
LIMIT @limit::int
OFFSET @offset::int;
