-- name: gate_request__get :one
SELECT
    id, risk_assessment_id, gate_policy_id, target_type, target_ref,
    interaction_delivery_ref, evidence_refs, evidence_summary, status,
    terminal_actor_ref, terminal_reason, terminal_at,
    version, created_at, updated_at
FROM governance_manager_gate_requests
WHERE id = @id;
