-- name: provider_credential__historical_bootstrap_pin :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.provider_credential_revisions credential
    WHERE credential.organization_id = @organization_id::uuid
      AND credential.provider_account_id = @provider_account_id::uuid
      AND credential.secret_name = @secret_name
      AND credential.secret_uid = @secret_uid::uuid
      AND credential.secret_resource_version = @secret_resource_version
      AND credential.content_sha256 = @content_sha256
);
