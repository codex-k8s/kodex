-- name: provider_credential_cleanup_lock_task :one
SELECT task.state,
       COALESCE(task.lease_owner, ''),
       task.lease_generation,
       task.lease_expires_at,
       task.attempts,
       task.maximum_attempts,
       task.safe_error_code,
       task.terminal_receipt
FROM control_plane.provider_credential_cleanup_tasks task
WHERE task.ref = @task_ref
FOR UPDATE;
