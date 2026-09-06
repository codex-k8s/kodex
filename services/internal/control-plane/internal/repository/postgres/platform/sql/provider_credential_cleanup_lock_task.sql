-- name: provider_credential_cleanup_lock_task :one
SELECT task.state,
       COALESCE(task.lease_owner, ''),
       task.lease_generation,
       task.lease_expires_at,
       task.attempts,
       task.maximum_attempts,
       task.safe_error_code,
       task.terminal_receipt,
       task.target_kind,
       COALESCE(auth_attempt.ref, ''),
       COALESCE(task.materializer_attempt_ref, ''),
       task.completion_descriptor
FROM control_plane.provider_credential_cleanup_tasks task
LEFT JOIN control_plane.provider_authorization_attempts auth_attempt
  ON auth_attempt.id = task.provider_authorization_attempt_id
 AND auth_attempt.provider_account_id = task.provider_account_id
 AND auth_attempt.organization_id = task.organization_id
WHERE task.ref = @task_ref
FOR UPDATE OF task;
