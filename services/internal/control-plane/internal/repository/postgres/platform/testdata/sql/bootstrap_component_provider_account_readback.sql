-- name: bootstrap_component_provider_account_readback :one
SELECT account.state,
       account.current_credential_revision_id::text,
       account.version
FROM control_plane.provider_accounts account
WHERE account.id = $1::uuid;
