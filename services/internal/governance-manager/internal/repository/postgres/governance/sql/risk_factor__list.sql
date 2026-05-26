-- name: risk_factor__list :many
SELECT
    id, risk_assessment_id, source_type, source_ref, risk_class, summary, created_at
FROM governance_manager_risk_factors
WHERE risk_assessment_id = @risk_assessment_id
  AND (@source_type::text = '' OR source_type = @source_type)
ORDER BY created_at, id
LIMIT @limit::integer OFFSET @offset::integer;
