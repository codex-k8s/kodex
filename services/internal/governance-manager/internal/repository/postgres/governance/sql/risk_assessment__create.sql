-- name: risk_assessment__create :exec
INSERT INTO governance_manager_risk_assessments (
    id, target_type, target_ref, project_ref, repository_ref, service_ref,
    branch_rules_ref, release_policy_ref, release_line_ref, provider_context,
    agent_context, runtime_context, initial_risk_class, effective_risk_class,
    status, explanation, required_gates, version, created_at, updated_at
) VALUES (
    @id, @target_type, @target_ref, @project_ref, @repository_ref, @service_ref,
    @branch_rules_ref, @release_policy_ref, @release_line_ref, @provider_context::jsonb,
    @agent_context::jsonb, @runtime_context::jsonb, @initial_risk_class, @effective_risk_class,
    @status, @explanation, @required_gates::jsonb, @version, @created_at, @updated_at
);
