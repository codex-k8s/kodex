-- name: runtime_commitprovidercredentialrefresh_insert_revision :one
INSERT INTO control_plane.provider_credential_revisions (
    ref, organization_id, provider_account_id, revision_number, secret_name,
    secret_uid, secret_resource_version, content_sha256, observed_at
)
SELECT @ref, @organization_id::uuid, account.id,
       COALESCE(maximum.revision_number, 0) + 1,
       @secret_name, @secret_uid::uuid, @secret_resource_version, @content_sha256,
       clock_timestamp()
FROM control_plane.provider_accounts account
LEFT JOIN LATERAL (
    SELECT max(revision.revision_number) AS revision_number
    FROM control_plane.provider_credential_revisions revision
    WHERE revision.provider_account_id = account.id
) maximum ON true
WHERE account.id = @provider_account_id::uuid
  AND account.organization_id = @organization_id::uuid
RETURNING id::text, revision_number;
