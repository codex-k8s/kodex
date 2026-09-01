-- name: provider_credential_cleanup_fail_task :exec
UPDATE control_plane.provider_credential_cleanup_tasks task
SET state = @state,
    eligible_at = @eligible_at,
    lease_owner = NULL,
    lease_expires_at = NULL,
    safe_error_code = @safe_error_code,
    terminal_receipt = @terminal_receipt,
    completed_at = @completed_at,
    updated_at = clock_timestamp()
WHERE task.ref = @task_ref
  AND task.state = 'CLAIMED'
  AND task.lease_owner = @lease_owner
  AND task.lease_generation = @lease_generation
  AND task.lease_expires_at > clock_timestamp();
