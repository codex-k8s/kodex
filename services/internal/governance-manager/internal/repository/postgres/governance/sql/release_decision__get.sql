-- name: release_decision__get :one
SELECT
    id, release_decision_package_id, gate_decision_id, outcome,
    decision_actor_ref, decision_policy_ref, reason, conditions_summary,
    status, version, decided_at, created_at, updated_at
FROM governance_manager_release_decisions
WHERE id = @id;
