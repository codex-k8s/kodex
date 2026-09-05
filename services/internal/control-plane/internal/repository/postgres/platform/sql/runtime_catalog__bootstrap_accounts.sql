-- name: runtime_catalog__bootstrap_accounts :many
SELECT account.ref
FROM control_plane.provider_accounts account
WHERE account.organization_id = $1::uuid AND account.definition_key = $2
  AND account.enabled AND account.state = 'AUTHORIZED'
  AND account.current_credential_revision_id IS NOT NULL
ORDER BY account.ref LIMIT 32;
