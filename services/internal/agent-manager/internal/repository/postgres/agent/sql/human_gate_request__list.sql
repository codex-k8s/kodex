-- name: human_gate_request__list :many
SELECT
    id,
    session_id,
    run_id,
    stage_id,
    acceptance_result_id,
    provider_work_item_ref,
    provider_pull_request_ref,
    provider_comment_ref,
    provider_review_signal_ref,
    target_ref,
    request_kind,
    reason_code,
    safe_summary,
    interaction_request_ref,
    interaction_response_ref,
    governance_gate_request_ref,
    governance_decision_ref,
    governance_risk_assessment_ref,
    governance_release_decision_package_ref,
    governance_release_decision_ref,
    governance_risk_profile_ref,
    governance_gate_policy_ref,
    governance_release_policy_ref,
    idempotency_key,
    status,
    outcome,
    version,
    resolved_at,
    created_at,
    updated_at
FROM agent_manager_human_gate_requests
WHERE (@session_id::uuid IS NULL OR session_id = @session_id::uuid)
  AND (@run_id::uuid IS NULL OR run_id = @run_id::uuid)
  AND (@stage_id::uuid IS NULL OR stage_id = @stage_id::uuid)
  AND (@status::text IS NULL OR status = @status::text)
  AND (@outcome::text IS NULL OR outcome = @outcome::text)
ORDER BY updated_at DESC, id DESC
LIMIT @limit::int
OFFSET @offset::int;
