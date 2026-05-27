-- name: human_gate_request__create :exec
INSERT INTO agent_manager_human_gate_requests (
    id, session_id, run_id, stage_id, acceptance_result_id,
    provider_work_item_ref, provider_pull_request_ref, provider_comment_ref,
    provider_review_signal_ref, target_ref, request_kind, reason_code, safe_summary,
    interaction_request_ref, interaction_response_ref, governance_gate_request_ref,
    governance_decision_ref, idempotency_key, status, outcome, version, resolved_at,
    created_at, updated_at
) VALUES (
    @id, @session_id, @run_id::uuid, @stage_id::uuid, @acceptance_result_id::uuid,
    @provider_work_item_ref, @provider_pull_request_ref, @provider_comment_ref,
    @provider_review_signal_ref, @target_ref, @request_kind, @reason_code, @safe_summary,
    @interaction_request_ref, @interaction_response_ref, @governance_gate_request_ref,
    @governance_decision_ref, @idempotency_key, @status, @outcome, @version, @resolved_at,
    @created_at, @updated_at
);
