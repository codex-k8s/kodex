-- name: provider_cleanup_lock_account :one
SELECT account.organization_id::text, account.id::text, account.ref
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_cleanup_tasks task
  ON task.provider_account_id = account.id AND task.organization_id = account.organization_id
WHERE task.ref = $1
FOR UPDATE OF account;
