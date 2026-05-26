-- name: risk_assessment__get :one
SELECT
    id, target_type, target_ref, project_ref, repository_ref, service_ref,
    branch_rules_ref, release_policy_ref, release_line_ref, provider_context,
    agent_context, runtime_context, initial_risk_class, effective_risk_class,
    status, explanation, required_gates, version, created_at, updated_at
FROM governance_manager_risk_assessments
WHERE id = @id;
