-- name: runtime_commitprovidercredentialrefresh_select_existing_revision :one
SELECT id::text, ref, revision_number, secret_name, secret_uid::text,
       secret_resource_version, content_sha256
FROM control_plane.provider_credential_revisions
WHERE provider_account_id = $1::uuid
  AND secret_uid = $2::uuid
  AND secret_resource_version = $3;
