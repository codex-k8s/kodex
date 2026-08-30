-- name: provider_accounts_materialization_referenced :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.provider_accounts account
    WHERE account.organization_id = @organization_id::uuid
      AND account.ref = @account_ref
      AND (
          (
              @materializer_attempt_ref <> ''
              AND EXISTS (
                  SELECT 1
                  FROM control_plane.provider_authorization_attempts auth_attempt
                  WHERE auth_attempt.organization_id = account.organization_id
                    AND auth_attempt.provider_account_id = account.id
                    AND auth_attempt.ref = @authorization_ref
                    AND auth_attempt.materializer_attempt_ref = @materializer_attempt_ref
              )
          )
          OR (
              @secret_name <> ''
              AND EXISTS (
                  SELECT 1
                  FROM control_plane.provider_credential_revisions credential
                  WHERE credential.organization_id = account.organization_id
                    AND credential.provider_account_id = account.id
                    AND credential.secret_name = @secret_name
                    AND credential.secret_uid::text = @secret_uid
                    AND credential.secret_resource_version = @secret_resource_version
                    AND credential.content_sha256 = @content_sha256
              )
          )
      )
);
