-- name: review_signal__get_by_fingerprint :one
SELECT
    id, risk_assessment_id, target_type, target_ref, role_kind, author_ref,
    outcome, severity, confidence, evidence_refs, summary, source_fingerprint, created_at
FROM governance_manager_review_signals
WHERE source_fingerprint = @source_fingerprint
  AND source_fingerprint <> '';
