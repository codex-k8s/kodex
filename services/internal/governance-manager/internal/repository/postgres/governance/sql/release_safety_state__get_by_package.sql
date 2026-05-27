-- name: release_safety_state__get_by_package :one
SELECT
    id, release_decision_package_id, current_state, runtime_job_ref,
    blocking_signal_count, last_state_reason, version, created_at, updated_at
FROM governance_manager_release_safety_states
WHERE release_decision_package_id = @release_decision_package_id;
