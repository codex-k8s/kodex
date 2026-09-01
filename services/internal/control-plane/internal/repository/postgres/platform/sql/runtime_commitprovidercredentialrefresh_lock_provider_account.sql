-- name: runtime_commitprovidercredentialrefresh_lock_provider_account :one
SELECT account.id::text, account.current_credential_revision_id::text
FROM control_plane.provider_accounts account
WHERE account.organization_id = $1::uuid
  AND account.state = 'AUTHORIZED'
  AND account.enabled
  AND account.id = (
      SELECT revision.provider_account_id
      FROM control_plane.runtime_revisions revision
      WHERE revision.id = $2::uuid
        AND revision.organization_id = $1::uuid
  )
FOR UPDATE;
