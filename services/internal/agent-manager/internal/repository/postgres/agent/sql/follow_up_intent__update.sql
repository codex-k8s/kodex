-- name: follow_up_intent__update :exec
UPDATE agent_manager_follow_up_intents
SET
    run_id = @run_id::uuid,
    from_stage_id = @from_stage_id::uuid,
    to_stage_id = @to_stage_id::uuid,
    acceptance_result_id = @acceptance_result_id::uuid,
    provider_work_item_ref = @provider_work_item_ref,
    provider_pull_request_ref = @provider_pull_request_ref,
    provider_comment_ref = @provider_comment_ref,
    provider_review_signal_ref = @provider_review_signal_ref,
    provider_work_item_type = @provider_work_item_type,
    provider_operation_ref = @provider_operation_ref,
    instruction_body_digest = @instruction_body_digest,
    safe_title = @safe_title,
    safe_summary = @safe_summary,
    role_hint = @role_hint,
    stage_hint = @stage_hint,
    idempotency_key = @idempotency_key,
    governance_risk_assessment_ref = @governance_risk_assessment_ref,
    governance_gate_request_ref = @governance_gate_request_ref,
    governance_gate_decision_ref = @governance_gate_decision_ref,
    governance_release_decision_package_ref = @governance_release_decision_package_ref,
    governance_release_decision_ref = @governance_release_decision_ref,
    governance_risk_profile_ref = @governance_risk_profile_ref,
    governance_gate_policy_ref = @governance_gate_policy_ref,
    governance_release_policy_ref = @governance_release_policy_ref,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
