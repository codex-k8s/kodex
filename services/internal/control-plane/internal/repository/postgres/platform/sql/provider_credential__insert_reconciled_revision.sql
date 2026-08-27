-- name: provider_credential__insert_reconciled_revision :one
INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number, secret_name,
     secret_uid, secret_resource_version, content_sha256, observed_at)
VALUES
    (@ref, @organization_id::uuid, @provider_account_id::uuid, @revision_number,
     @secret_name, @secret_uid::uuid, @secret_resource_version, @content_sha256,
     clock_timestamp())
RETURNING id::text;
