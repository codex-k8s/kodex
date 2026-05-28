-- name: follow_up_intent__create :exec
INSERT INTO agent_manager_follow_up_intents (
    id, session_id, run_id, from_stage_id, to_stage_id, acceptance_result_id,
    provider_work_item_ref, provider_pull_request_ref, provider_comment_ref,
    provider_review_signal_ref, provider_work_item_type, provider_operation_ref,
    instruction_body_digest, safe_title, safe_summary, role_hint, stage_hint,
    idempotency_key, governance_risk_assessment_ref, governance_gate_request_ref,
    governance_gate_decision_ref, governance_release_decision_package_ref,
    governance_release_decision_ref, governance_risk_profile_ref,
    governance_gate_policy_ref, governance_release_policy_ref,
    status, version, created_at, updated_at
) VALUES (
    @id, @session_id, @run_id::uuid, @from_stage_id::uuid, @to_stage_id::uuid, @acceptance_result_id::uuid,
    @provider_work_item_ref, @provider_pull_request_ref, @provider_comment_ref,
    @provider_review_signal_ref, @provider_work_item_type, @provider_operation_ref,
    @instruction_body_digest, @safe_title, @safe_summary, @role_hint, @stage_hint,
    @idempotency_key, @governance_risk_assessment_ref, @governance_gate_request_ref,
    @governance_gate_decision_ref, @governance_release_decision_package_ref,
    @governance_release_decision_ref, @governance_risk_profile_ref,
    @governance_gate_policy_ref, @governance_release_policy_ref,
    @status, @version, @created_at, @updated_at
);
