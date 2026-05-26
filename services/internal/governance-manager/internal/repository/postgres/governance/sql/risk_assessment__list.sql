-- name: risk_assessment__list :many
SELECT
    id, target_type, target_ref, project_ref, repository_ref, service_ref,
    branch_rules_ref, release_policy_ref, release_line_ref, provider_context,
    agent_context, runtime_context, initial_risk_class, effective_risk_class,
    status, explanation, required_gates, version, created_at, updated_at
FROM governance_manager_risk_assessments
WHERE (@target_type::text = '' OR target_type = @target_type)
  AND (@target_ref::text = '' OR target_ref = @target_ref)
  AND (@project_ref::text = '' OR project_ref = @project_ref)
  AND (@repository_ref::text = '' OR repository_ref = @repository_ref)
  AND (@effective_risk_class::text = '' OR effective_risk_class = @effective_risk_class)
  AND (@status::text = '' OR status = @status)
ORDER BY updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
