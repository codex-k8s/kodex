-- name: provider_accounts_lock :one
SELECT account.id::text, account.version, account.state, account.enabled,
       account.current_credential_revision_id::text
FROM control_plane.provider_accounts account
WHERE account.organization_id = @organization_id::uuid
  AND account.ref = @account_ref
FOR UPDATE;
