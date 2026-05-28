-- name: acceptance_result__update :exec
UPDATE agent_manager_acceptance_results
SET
    session_id = @session_id,
    run_id = @run_id::uuid,
    stage_id = @stage_id::uuid,
    check_kind = @check_kind,
    status = @status,
    target_ref = @target_ref,
    details_json = @details_json::jsonb,
    governance_risk_assessment_ref = @governance_risk_assessment_ref,
    governance_gate_request_ref = @governance_gate_request_ref,
    governance_gate_decision_ref = @governance_gate_decision_ref,
    governance_release_decision_package_ref = @governance_release_decision_package_ref,
    governance_release_decision_ref = @governance_release_decision_ref,
    governance_risk_profile_ref = @governance_risk_profile_ref,
    governance_gate_policy_ref = @governance_gate_policy_ref,
    governance_release_policy_ref = @governance_release_policy_ref,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
