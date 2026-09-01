-- name: runtime_commitprovidercredentialrefresh_select_pinned_credential :one
SELECT account.ref,
       pinned.id::text,
       pinned.ref,
       pinned.content_sha256
FROM control_plane.runtime_revisions revision
JOIN control_plane.provider_accounts account
  ON account.id = revision.provider_account_id
 AND account.organization_id = revision.organization_id
JOIN control_plane.provider_credential_revisions pinned
  ON pinned.id = revision.provider_credential_revision_id
 AND pinned.provider_account_id = account.id
 AND pinned.organization_id = account.organization_id
WHERE revision.organization_id = $1::uuid
  AND revision.id = $2::uuid
  AND account.id = $3::uuid;
