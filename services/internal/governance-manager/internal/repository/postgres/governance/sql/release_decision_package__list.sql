-- name: release_decision_package__list :many
SELECT
    id, release_candidate_ref, project_ref, repository_ref, service_ref,
    branch_rules_ref, release_policy_ref, release_line_ref, repository_refs,
    risk_assessment_id, provider_refs, runtime_refs, agent_context,
    review_signal_ids, evidence_refs, integration_refs, known_limitations_summary, status,
    version, created_at, updated_at
FROM governance_manager_release_decision_packages
WHERE (@project_ref::text = '' OR project_ref = @project_ref)
  AND (@release_candidate_ref::text = '' OR release_candidate_ref = @release_candidate_ref)
  AND (@status::text = '' OR status = @status)
ORDER BY updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
