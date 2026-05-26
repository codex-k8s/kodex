-- name: release_decision_package__get :one
SELECT
    id, release_candidate_ref, project_ref, repository_ref, service_ref,
    branch_rules_ref, release_policy_ref, release_line_ref, repository_refs,
    risk_assessment_id, provider_refs, runtime_refs, agent_context,
    review_signal_ids, evidence_refs, known_limitations_summary, status,
    version, created_at, updated_at
FROM governance_manager_release_decision_packages
WHERE id = @id;
