-- name: provider_credential_cleanup_claim_task :one
UPDATE control_plane.provider_credential_cleanup_tasks task
SET state = 'CLAIMED',
    lease_owner = @lease_owner,
    lease_generation = task.lease_generation + 1,
    lease_expires_at = @lease_expires_at,
    attempts = task.attempts + 1,
    safe_error_code = '',
    updated_at = clock_timestamp()
WHERE task.id = @task_id::uuid
  AND task.attempts < task.maximum_attempts
  AND (
      (task.state = 'PENDING' AND task.eligible_at <= clock_timestamp())
      OR (task.state = 'CLAIMED' AND task.lease_expires_at <= clock_timestamp())
  )
RETURNING task.attempts, task.lease_generation, task.lease_expires_at;
