-- name: risk_rule__list :many
SELECT
    id, risk_profile_id, profile_version, rule_kind, matcher, min_risk_class,
    required_gate_policy_id, reason_template, status, created_at, updated_at
FROM governance_manager_risk_rules
WHERE risk_profile_id = @risk_profile_id
  AND profile_version = @profile_version
  AND (@rule_kind::text = '' OR rule_kind = @rule_kind)
  AND (@status::text = '' OR status = @status)
ORDER BY created_at, id
LIMIT @limit::integer OFFSET @offset::integer;
