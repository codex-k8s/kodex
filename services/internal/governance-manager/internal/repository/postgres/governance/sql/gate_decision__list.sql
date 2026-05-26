-- name: gate_decision__list :many
SELECT
    decision.id, decision.gate_request_id, decision.decision_actor_ref,
    decision.decision_policy_ref, decision.outcome, decision.reason,
    decision.conditions_summary, decision.source_ref, decision.decided_at
FROM governance_manager_gate_decisions AS decision
JOIN governance_manager_gate_requests AS request ON request.id = decision.gate_request_id
WHERE (@gate_request_id::uuid IS NULL OR decision.gate_request_id = @gate_request_id)
  AND (@target_type::text = '' OR request.target_type = @target_type)
  AND (@target_ref::text = '' OR request.target_ref = @target_ref)
  AND (@outcome::text = '' OR decision.outcome = @outcome)
ORDER BY decision.decided_at DESC, decision.id
LIMIT @limit::integer OFFSET @offset::integer;
