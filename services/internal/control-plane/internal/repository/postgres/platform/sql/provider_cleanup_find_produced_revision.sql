-- name: provider_cleanup_find_produced_revision :one
SELECT revision.id::text, revision.id = account.current_credential_revision_id,
       COALESCE(cleanup.ref, ''), COALESCE(cleanup.state, ''),
       COALESCE(cleanup.state='COMPLETED' AND cleanup.target_kind='CREDENTIAL'
         AND cleanup.completed_at IS NOT NULL AND cleanup.safe_error_code=''
         AND cleanup.terminal_receipt<>'' AND cleanup.terminal_receipt NOT LIKE 'superseded:%'
         AND cleanup.completion_descriptor->>'TerminalReceipt'=cleanup.terminal_receipt
         AND cleanup.secret_name=revision.secret_name AND cleanup.secret_uid=revision.secret_uid
         AND cleanup.secret_resource_version=revision.secret_resource_version
         AND cleanup.content_sha256=revision.content_sha256, false)
FROM control_plane.provider_credential_revisions revision
JOIN control_plane.provider_accounts account
  ON account.id = revision.provider_account_id AND account.organization_id = revision.organization_id
LEFT JOIN control_plane.provider_credential_cleanup_tasks cleanup
  ON cleanup.provider_credential_revision_id = revision.id
 AND cleanup.organization_id = revision.organization_id
 AND cleanup.provider_account_id = revision.provider_account_id
WHERE revision.organization_id = @organization_id::uuid
  AND revision.provider_account_id = @account_id::uuid
  AND revision.secret_name = @secret_name
  AND revision.secret_uid = @secret_uid::uuid
  AND revision.secret_resource_version = @secret_resource_version
  AND revision.content_sha256 = @content_sha256
ORDER BY revision.revision_number DESC, cleanup.created_at DESC, cleanup.id DESC
LIMIT 1;
