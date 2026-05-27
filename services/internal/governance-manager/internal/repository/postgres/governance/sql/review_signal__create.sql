-- name: review_signal__create :exec
INSERT INTO governance_manager_review_signals (
    id, risk_assessment_id, target_type, target_ref, role_kind, author_ref,
    outcome, severity, confidence, evidence_refs, summary, source_fingerprint, created_at
) VALUES (
    @id, @risk_assessment_id, @target_type, @target_ref, @role_kind, @author_ref,
    @outcome, @severity, @confidence, @evidence_refs::jsonb, @summary, @source_fingerprint, @created_at
);
