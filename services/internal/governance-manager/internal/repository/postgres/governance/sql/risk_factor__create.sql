-- name: risk_factor__create :exec
INSERT INTO governance_manager_risk_factors (
    id, risk_assessment_id, source_type, source_ref, risk_class, summary, created_at
) VALUES (
    @id, @risk_assessment_id, @source_type, @source_ref, @risk_class, @summary, @created_at
);
