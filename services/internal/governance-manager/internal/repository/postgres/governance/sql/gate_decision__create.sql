-- name: gate_decision__create :exec
INSERT INTO governance_manager_gate_decisions (
    id, gate_request_id, decision_actor_ref, decision_policy_ref, outcome,
    reason, conditions_summary, source_ref, decided_at
) VALUES (
    @id, @gate_request_id, @decision_actor_ref, @decision_policy_ref, @outcome,
    @reason, @conditions_summary, @source_ref, @decided_at
);
