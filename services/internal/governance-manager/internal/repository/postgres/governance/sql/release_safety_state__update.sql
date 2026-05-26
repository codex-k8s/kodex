-- name: release_safety_state__update :exec
UPDATE governance_manager_release_safety_states
SET
    current_state = @current_state,
    runtime_job_ref = @runtime_job_ref,
    blocking_signal_count = @blocking_signal_count,
    last_state_reason = @last_state_reason,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
