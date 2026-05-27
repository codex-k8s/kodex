-- name: release_decision__update :exec
UPDATE governance_manager_release_decisions
SET
    gate_decision_id = @gate_decision_id,
    outcome = @outcome,
    decision_actor_ref = @decision_actor_ref,
    decision_policy_ref = @decision_policy_ref,
    reason = @reason,
    conditions_summary = @conditions_summary,
    status = @status,
    version = @version,
    decided_at = @decided_at,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
