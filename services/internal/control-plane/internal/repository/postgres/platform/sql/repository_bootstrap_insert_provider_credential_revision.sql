-- name: platform__repository_bootstrap_insert_provider_credential_revision :one
INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number, secret_name,
     secret_uid, secret_resource_version, content_sha256, observed_at)
VALUES
    ($1, $2::uuid, $3::uuid, 1, $4, $5::uuid, $6, $7, clock_timestamp())
RETURNING id::text;
