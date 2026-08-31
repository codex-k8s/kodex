-- name: provider_credential_cleanup_select_claimable_tasks :many
SELECT task.id::text,
       task.ref,
       account.ref,
       task.secret_name,
       task.secret_uid::text,
       task.secret_resource_version,
       task.content_sha256
FROM control_plane.provider_credential_cleanup_tasks task
JOIN control_plane.provider_accounts account
  ON account.id = task.provider_account_id
 AND account.organization_id = task.organization_id
WHERE task.provider_account_id = @account_id::uuid
  AND task.attempts < task.maximum_attempts
  AND (
      (task.state = 'PENDING' AND task.eligible_at <= clock_timestamp())
      OR (task.state = 'CLAIMED' AND task.lease_expires_at <= clock_timestamp())
  )
  AND (
      account.state = 'REVOKED'
      OR task.provider_credential_revision_id IS DISTINCT FROM account.current_credential_revision_id
  )
ORDER BY task.eligible_at, task.created_at, task.id
FOR UPDATE OF task SKIP LOCKED
LIMIT @limit;
