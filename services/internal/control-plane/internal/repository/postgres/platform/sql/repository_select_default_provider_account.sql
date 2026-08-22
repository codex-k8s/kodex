-- name: platform__repository_select_default_provider_account :one
SELECT id::text
FROM control_plane.provider_accounts
WHERE organization_id = $1::uuid
  AND stable_key = 'default-openai-codex'
  AND state = 'AUTHORIZED'
  AND enabled
  AND current_credential_revision_id IS NOT NULL;
