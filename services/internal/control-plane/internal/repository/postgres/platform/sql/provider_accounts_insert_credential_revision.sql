-- name: provider_accounts_insert_credential_revision :one
INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number, secret_name,
     secret_uid, secret_resource_version, content_sha256, observed_at)
SELECT @credential_ref, @organization_id::uuid, @account_id::uuid,
       COALESCE(max(revision.revision_number), 0) + 1,
       @secret_name, @secret_uid::uuid, @secret_resource_version,
       @content_sha256, clock_timestamp()
FROM control_plane.provider_accounts account
LEFT JOIN control_plane.provider_credential_revisions revision
  ON revision.provider_account_id = account.id
WHERE account.id = @account_id::uuid
GROUP BY account.id
RETURNING id::text;
