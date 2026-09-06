-- name: provider_account_deletion_lock_pending :many
SELECT account.organization_id::text, account.id::text
FROM control_plane.provider_accounts account
JOIN control_plane.provider_account_deletion_intents intent
  ON intent.provider_account_id = account.id AND intent.organization_id = account.organization_id
WHERE account.state = 'DELETING' AND intent.state <> 'DELETED'
ORDER BY intent.updated_at, account.id
FOR UPDATE OF account SKIP LOCKED
LIMIT @limit;
