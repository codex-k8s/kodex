-- name: release_decision__list :many
SELECT
    decision.id, decision.release_decision_package_id, decision.gate_decision_id,
    decision.outcome, decision.decision_actor_ref, decision.decision_policy_ref,
    decision.reason, decision.conditions_summary, decision.status, decision.version,
    decision.decided_at, decision.created_at, decision.updated_at
FROM governance_manager_release_decisions AS decision
JOIN governance_manager_release_decision_packages AS package
  ON package.id = decision.release_decision_package_id
WHERE (@release_decision_package_id::uuid IS NULL OR decision.release_decision_package_id = @release_decision_package_id)
  AND (@project_ref::text = '' OR package.project_ref = @project_ref)
  AND (@status::text = '' OR decision.status = @status)
  AND (@outcome::text = '' OR decision.outcome = @outcome)
ORDER BY decision.updated_at DESC, decision.id
LIMIT @limit::integer OFFSET @offset::integer;
