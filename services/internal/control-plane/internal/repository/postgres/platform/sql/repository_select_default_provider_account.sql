-- name: repository_select_default_provider_account :one
SELECT account.id::text
FROM control_plane.provider_accounts account
LEFT JOIN control_plane.sessions session
  ON session.provider_account_id = account.id
WHERE account.organization_id = $1::uuid
  AND account.definition_key = 'openai-codex'
  AND account.state = 'AUTHORIZED'
  AND account.enabled
  AND account.current_credential_revision_id IS NOT NULL
GROUP BY account.id, account.created_at
ORDER BY count(session.id), account.created_at, account.id
LIMIT 1;
