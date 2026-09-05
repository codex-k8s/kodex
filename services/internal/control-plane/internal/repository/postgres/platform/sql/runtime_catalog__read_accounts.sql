-- name: runtime_catalog__read_accounts :one
SELECT account.ref
FROM control_plane.provider_accounts account
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE account.organization_id = @organization_id::uuid AND account.ref = @account_ref
  AND definition.stable_key = @provider AND definition.enabled
  AND account.enabled AND account.state = 'AUTHORIZED'
  AND account.current_credential_revision_id IS NOT NULL;
