-- name: bootstrap_component_rotate_provider_credential :one
WITH current_credential AS (
    SELECT account.id AS account_id,
           account.organization_id,
           account.current_credential_revision_id,
           credential.revision_number,
           credential.secret_name
    FROM control_plane.provider_accounts account
    JOIN control_plane.provider_credential_revisions credential
      ON credential.id = account.current_credential_revision_id
    WHERE account.id = $1::uuid
    FOR UPDATE OF account
), inserted AS (
    INSERT INTO control_plane.provider_credential_revisions
        (ref, organization_id, provider_account_id, revision_number,
         secret_name, secret_uid, secret_resource_version, content_sha256,
         observed_at)
    SELECT 'pcr_component_reauth_' || replace(gen_random_uuid()::text, '-', ''),
           current_credential.organization_id,
           current_credential.account_id,
           current_credential.revision_number + 1,
           current_credential.secret_name,
           $2::uuid,
           $3::text,
           $4::text,
           clock_timestamp()
    FROM current_credential
    RETURNING id, provider_account_id
)
UPDATE control_plane.provider_accounts account
SET current_credential_revision_id = inserted.id,
    state = 'AUTHORIZED',
    version = account.version + 1,
    updated_at = clock_timestamp()
FROM current_credential, inserted
WHERE account.id = current_credential.account_id
  AND account.id = inserted.provider_account_id
  AND account.current_credential_revision_id = current_credential.current_credential_revision_id
RETURNING inserted.id::text;
