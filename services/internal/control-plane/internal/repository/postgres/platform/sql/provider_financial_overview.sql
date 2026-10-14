-- name: provider_financial_overview :many
SELECT account.ref, account.name,
       CASE WHEN account.current_credential_revision_id = account.financial_credential_revision_id
         AND account.state = 'AUTHORIZED' AND account.enabled THEN account.financial_observation END,
       account.financial_received_at, account.version
FROM control_plane.provider_accounts account
WHERE account.organization_id = @organization_id::uuid
  AND account.definition_key = 'openai-codex' AND account.state <> 'DELETED'
ORDER BY account.ref;
