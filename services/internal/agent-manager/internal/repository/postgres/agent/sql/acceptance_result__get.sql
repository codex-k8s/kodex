-- name: acceptance_result__get :one
SELECT
    id,
    session_id,
    run_id,
    stage_id,
    check_kind,
    status,
    target_ref,
    details_json,
    governance_risk_assessment_ref,
    governance_gate_request_ref,
    governance_gate_decision_ref,
    governance_release_decision_package_ref,
    governance_release_decision_ref,
    governance_risk_profile_ref,
    governance_gate_policy_ref,
    governance_release_policy_ref,
    version,
    created_at,
    updated_at
FROM agent_manager_acceptance_results
WHERE id = @id;
