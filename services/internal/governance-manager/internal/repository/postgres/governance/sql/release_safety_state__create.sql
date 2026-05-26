-- name: release_safety_state__create :exec
INSERT INTO governance_manager_release_safety_states (
    id, release_decision_package_id, current_state, runtime_job_ref,
    blocking_signal_count, last_state_reason, version, created_at, updated_at
) VALUES (
    @id, @release_decision_package_id, @current_state, @runtime_job_ref,
    @blocking_signal_count, @last_state_reason, @version, @created_at, @updated_at
);
