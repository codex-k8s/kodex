-- name: provider_account_deletion_lock_pending :many
SELECT account.organization_id::text, organization.ref, account.id::text
FROM control_plane.provider_accounts account
JOIN control_plane.organizations organization ON organization.id = account.organization_id
JOIN control_plane.provider_account_deletion_intents intent
  ON intent.provider_account_id = account.id AND intent.organization_id = account.organization_id
WHERE account.state = 'DELETING' AND intent.state <> 'DELETED'
ORDER BY intent.updated_at, account.id
FOR UPDATE OF account SKIP LOCKED
LIMIT @limit;
