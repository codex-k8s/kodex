-- name: provider_renewal__claim_accounts :many
SELECT account.id::text, account.ref, account.version, credential.id::text, credential.ref, credential.revision_number,
       credential.secret_name, credential.secret_uid::text, credential.secret_resource_version, credential.content_sha256,
       clock_timestamp() + interval '90 seconds',
       COALESCE((SELECT max(task.attempt) FROM control_plane.provider_credential_renewal_tasks task WHERE task.provider_account_id = account.id), 0) + 1
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_revisions credential ON credential.id = account.current_credential_revision_id
    AND credential.provider_account_id = account.id AND credential.organization_id = account.organization_id
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE account.organization_id = $1::uuid AND account.state = 'AUTHORIZED' AND account.enabled AND definition.enabled
    AND account.definition_key = 'openai-codex'
    AND COALESCE((SELECT auth.method FROM control_plane.provider_authorization_attempts auth
        WHERE auth.provider_account_id = account.id AND auth.state = 'AUTHORIZED' ORDER BY auth.updated_at DESC, auth.ref DESC LIMIT 1), 'DEVICE_CODE') = 'DEVICE_CODE'
    AND (SELECT observation.failure FROM control_plane.provider_model_catalog_observations observation
        WHERE observation.provider_account_id = account.id AND observation.account_version = account.version
          AND observation.provider_credential_revision_id = credential.id ORDER BY observation.created_at DESC, observation.id DESC LIMIT 1) = 'AUTHORIZATION_REJECTED'
    AND NOT EXISTS (SELECT 1 FROM control_plane.provider_credential_renewal_tasks task
        WHERE task.provider_account_id = account.id AND (task.state IN ('CLAIMED', 'AUTHORIZED')
            OR (task.input_credential_id = credential.id AND task.authorized_at IS NOT NULL)))
ORDER BY account.updated_at, account.id
LIMIT 1
FOR UPDATE OF account SKIP LOCKED;
