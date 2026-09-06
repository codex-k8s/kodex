-- name: provider_credential_cleanup_select_claimable_tasks :many
SELECT task.id::text,
       task.ref,
       account.ref,
       COALESCE(task.secret_name, ''),
       COALESCE(task.secret_uid::text, ''),
       COALESCE(task.secret_resource_version, ''),
       COALESCE(task.content_sha256, ''),
       task.target_kind,
       COALESCE(auth_attempt.ref, ''),
       COALESCE(task.materializer_attempt_ref, ''),
       COALESCE(task.materializer_attempt_uid::text, ''),
       COALESCE(task.materializer_attempt_resource_version, ''),
       task.recovery_task_ref,task.recovery_generation,task.recovery_legacy_last_generation
FROM control_plane.provider_credential_cleanup_tasks task
JOIN control_plane.provider_accounts account
  ON account.id = task.provider_account_id
 AND account.organization_id = task.organization_id
LEFT JOIN control_plane.provider_authorization_attempts auth_attempt
  ON auth_attempt.id = task.provider_authorization_attempt_id
 AND auth_attempt.organization_id = task.organization_id
 AND auth_attempt.provider_account_id = task.provider_account_id
WHERE task.provider_account_id = @account_id::uuid
  AND task.attempts < task.maximum_attempts
  AND (
      (task.state = 'PENDING' AND task.eligible_at <= clock_timestamp())
      OR (task.state = 'CLAIMED' AND task.lease_expires_at <= clock_timestamp())
  )
  AND control_plane.provider_cleanup_task_eligible(task.id)
ORDER BY task.eligible_at, task.created_at, task.id
FOR UPDATE OF task SKIP LOCKED
LIMIT @limit;
