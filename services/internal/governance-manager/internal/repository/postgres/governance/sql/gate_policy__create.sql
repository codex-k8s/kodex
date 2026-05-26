-- name: gate_policy__create :exec
INSERT INTO governance_manager_gate_policies (
    id, risk_profile_id, profile_version, gate_kind, min_risk_class,
    required_actor_policy_ref, required_signal_kinds, timeout_policy_ref, status
) VALUES (
    @id, @risk_profile_id, @profile_version, @gate_kind, @min_risk_class,
    @required_actor_policy_ref, @required_signal_kinds::jsonb, @timeout_policy_ref, @status
);
