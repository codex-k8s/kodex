-- name: gate_policy__list :many
SELECT
    id, risk_profile_id, profile_version, gate_kind, min_risk_class,
    required_actor_policy_ref, required_signal_kinds, timeout_policy_ref, status
FROM governance_manager_gate_policies
WHERE risk_profile_id = @risk_profile_id
  AND profile_version = @profile_version
  AND (@gate_kind::text = '' OR gate_kind = @gate_kind)
  AND (@status::text = '' OR status = @status)
ORDER BY gate_kind, id
LIMIT @limit::integer OFFSET @offset::integer;
