-- name: risk_factor__delete_by_assessment :exec
DELETE FROM governance_manager_risk_factors
WHERE risk_assessment_id = @risk_assessment_id;
