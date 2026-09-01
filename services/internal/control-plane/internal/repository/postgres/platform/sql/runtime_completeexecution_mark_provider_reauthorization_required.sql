-- name: runtime_completeexecution_mark_provider_reauthorization_required :exec
UPDATE control_plane.provider_accounts account
SET state = 'REAUTHORIZATION_REQUIRED',
    version = account.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.runtime_revisions revision
WHERE revision.id = @runtime_revision_id::uuid
  AND revision.organization_id = @organization_id::uuid
  AND account.id = revision.provider_account_id
  AND account.organization_id = revision.organization_id
  AND account.current_credential_revision_id = revision.provider_credential_revision_id
  AND account.state = 'AUTHORIZED';
