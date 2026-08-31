-- name: provider_credential__get_current_for_reconcile :one
SELECT account.organization_id::text,
       account.id::text,
       credential.id::text,
       credential.revision_number,
       credential.secret_name,
       credential.secret_uid::text,
       credential.secret_resource_version,
       credential.content_sha256
FROM control_plane.owner_claim_contracts installation_owner
JOIN control_plane.provider_accounts account
  ON account.organization_id = installation_owner.organization_id
JOIN control_plane.provider_credential_revisions credential
  ON credential.id = account.current_credential_revision_id
WHERE installation_owner.stable_key = 'installation-owner'
  AND account.stable_key = 'default-openai-codex'
FOR UPDATE OF account;
