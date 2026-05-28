-- name: acceptance_result__list :many
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
WHERE (@session_id::uuid IS NULL OR session_id = @session_id::uuid)
  AND (@run_id::uuid IS NULL OR run_id = @run_id::uuid)
  AND (@stage_id::uuid IS NULL OR stage_id = @stage_id::uuid)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY updated_at DESC, id DESC
LIMIT @limit::int
OFFSET @offset::int;
