-- name: release_decision__create :exec
INSERT INTO governance_manager_release_decisions (
    id, release_decision_package_id, gate_decision_id, outcome,
    decision_actor_ref, decision_policy_ref, reason, conditions_summary,
    status, version, decided_at, created_at, updated_at
) VALUES (
    @id, @release_decision_package_id, @gate_decision_id, @outcome,
    @decision_actor_ref, @decision_policy_ref, @reason, @conditions_summary,
    @status, @version, @decided_at, @created_at, @updated_at
);
