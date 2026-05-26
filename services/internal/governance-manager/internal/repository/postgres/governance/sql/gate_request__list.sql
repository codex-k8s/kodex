-- name: gate_request__list :many
SELECT
    id, risk_assessment_id, gate_policy_id, target_type, target_ref,
    interaction_delivery_ref, evidence_refs, evidence_summary, status,
    terminal_actor_ref, terminal_reason, terminal_at,
    version, created_at, updated_at
FROM governance_manager_gate_requests
WHERE (@risk_assessment_id::uuid IS NULL OR risk_assessment_id = @risk_assessment_id)
  AND (@target_type::text = '' OR target_type = @target_type)
  AND (@target_ref::text = '' OR target_ref = @target_ref)
  AND (@status::text = '' OR status = @status)
ORDER BY updated_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
