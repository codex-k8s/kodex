-- name: review_signal__get :one
SELECT
    id, risk_assessment_id, target_type, target_ref, role_kind, author_ref,
    outcome, severity, confidence, evidence_refs, summary, created_at
FROM governance_manager_review_signals
WHERE id = @id;
