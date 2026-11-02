-- name: provider_authorization_definition :one
SELECT account.definition_key
FROM control_plane.provider_accounts account
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE account.id = @account_id::uuid AND definition.enabled;
