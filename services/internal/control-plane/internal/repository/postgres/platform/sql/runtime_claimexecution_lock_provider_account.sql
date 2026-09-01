-- name: runtime_claimexecution_lock_provider_account :one
SELECT max_concurrent_executions
FROM control_plane.provider_accounts
WHERE id = $1::uuid
  AND organization_id = $2::uuid
  AND state = 'AUTHORIZED'
  AND enabled
FOR UPDATE;
