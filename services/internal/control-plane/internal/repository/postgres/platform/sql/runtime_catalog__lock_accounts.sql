-- name: runtime_catalog__lock_accounts :one
SELECT account.ref
FROM control_plane.provider_accounts account
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE account.organization_id = $1::uuid AND account.ref = $2
  AND definition.stable_key = $3 AND definition.enabled
  AND account.enabled AND account.state = 'AUTHORIZED'
  AND account.current_credential_revision_id IS NOT NULL
FOR SHARE OF account, definition;
