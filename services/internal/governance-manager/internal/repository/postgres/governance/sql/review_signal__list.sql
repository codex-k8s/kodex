-- name: review_signal__list :many
SELECT
    id, risk_assessment_id, target_type, target_ref, role_kind, author_ref,
    outcome, severity, confidence, evidence_refs, summary, created_at
FROM governance_manager_review_signals
WHERE (@risk_assessment_id::uuid IS NULL OR risk_assessment_id = @risk_assessment_id)
  AND (@target_type::text = '' OR target_type = @target_type)
  AND (@target_ref::text = '' OR target_ref = @target_ref)
  AND (@role_kind::text = '' OR role_kind = @role_kind)
  AND (@outcome::text = '' OR outcome = @outcome)
ORDER BY created_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
