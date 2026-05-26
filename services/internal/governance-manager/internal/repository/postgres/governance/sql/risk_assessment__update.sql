-- name: risk_assessment__update :exec
UPDATE governance_manager_risk_assessments
SET
    project_ref = @project_ref,
    repository_ref = @repository_ref,
    service_ref = @service_ref,
    branch_rules_ref = @branch_rules_ref,
    release_policy_ref = @release_policy_ref,
    release_line_ref = @release_line_ref,
    provider_context = @provider_context::jsonb,
    agent_context = @agent_context::jsonb,
    runtime_context = @runtime_context::jsonb,
    risk_profile_id = @risk_profile_id,
    risk_profile_version = @risk_profile_version,
    evaluation_summary = @evaluation_summary::jsonb,
    evidence_refs = @evidence_refs::jsonb,
    initial_risk_class = @initial_risk_class,
    effective_risk_class = @effective_risk_class,
    status = @status,
    explanation = @explanation,
    required_gates = @required_gates::jsonb,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
