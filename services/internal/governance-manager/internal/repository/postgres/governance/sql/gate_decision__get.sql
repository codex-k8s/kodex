-- name: gate_decision__get :one
SELECT
    id, gate_request_id, decision_actor_ref, decision_policy_ref, outcome,
    reason, conditions_summary, source_ref, decided_at
FROM governance_manager_gate_decisions
WHERE id = @id;
