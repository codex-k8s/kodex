-- name: release_decision_package__create :exec
INSERT INTO governance_manager_release_decision_packages (
    id, release_candidate_ref, project_ref, repository_ref, service_ref,
    branch_rules_ref, release_policy_ref, release_line_ref, repository_refs,
    risk_assessment_id, provider_refs, runtime_refs, agent_context,
    review_signal_ids, evidence_refs, integration_refs, known_limitations_summary, status,
    version, created_at, updated_at
) VALUES (
    @id, @release_candidate_ref, @project_ref, @repository_ref, @service_ref,
    @branch_rules_ref, @release_policy_ref, @release_line_ref, @repository_refs,
    @risk_assessment_id, @provider_refs::jsonb, @runtime_refs::jsonb, @agent_context::jsonb,
    @review_signal_ids, @evidence_refs::jsonb, @integration_refs::jsonb, @known_limitations_summary, @status,
    @version, @created_at, @updated_at
);
