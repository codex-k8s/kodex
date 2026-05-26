-- name: risk_rule__create :exec
INSERT INTO governance_manager_risk_rules (
    id, risk_profile_id, profile_version, rule_kind, matcher, min_risk_class,
    required_gate_policy_id, reason_template, status, created_at, updated_at
) VALUES (
    @id, @risk_profile_id, @profile_version, @rule_kind, @matcher::jsonb, @min_risk_class,
    @required_gate_policy_id, @reason_template::jsonb, @status, @created_at, @updated_at
);
