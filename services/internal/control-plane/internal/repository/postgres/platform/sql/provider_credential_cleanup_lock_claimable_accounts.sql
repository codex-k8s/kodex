-- name: provider_credential_cleanup_lock_claimable_accounts :many
SELECT account.id::text, account.ref
FROM control_plane.provider_accounts account
WHERE EXISTS (
    SELECT 1
    FROM control_plane.provider_credential_cleanup_tasks task
    WHERE task.provider_account_id = account.id
      AND task.organization_id = account.organization_id
      AND task.attempts < task.maximum_attempts
      AND (
          (task.state = 'PENDING' AND task.eligible_at <= clock_timestamp())
          OR (task.state = 'CLAIMED' AND task.lease_expires_at <= clock_timestamp())
      )
      AND control_plane.provider_cleanup_task_eligible(task.id)
)
ORDER BY account.id
FOR UPDATE OF account SKIP LOCKED
LIMIT @limit;
