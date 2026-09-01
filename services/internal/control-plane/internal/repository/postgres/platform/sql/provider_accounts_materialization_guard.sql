-- name: provider_accounts_materialization_guard :one
SELECT account.id::text
FROM control_plane.provider_accounts account
WHERE account.organization_id = @organization_id::uuid
  AND account.ref = @account_ref
FOR UPDATE;
