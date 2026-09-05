-- name: model_catalog__claim_accounts :many
SELECT account.id::text, account.ref, account.organization_id::text, account.version,
       account.definition_key, credential.id::text, credential.ref, credential.revision_number,
       credential.secret_name, credential.secret_uid::text, credential.secret_resource_version,
       credential.content_sha256, COALESCE((
           SELECT attempt.method FROM control_plane.provider_authorization_attempts attempt
           WHERE attempt.provider_account_id = account.id AND attempt.state = 'AUTHORIZED'
           ORDER BY attempt.updated_at DESC, attempt.ref DESC LIMIT 1
       ), 'DEVICE_CODE')
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_revisions credential ON credential.id = account.current_credential_revision_id
  AND credential.provider_account_id = account.id AND credential.organization_id = account.organization_id
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE account.enabled AND account.state = 'AUTHORIZED' AND definition.enabled
  AND NOT EXISTS (SELECT 1 FROM control_plane.provider_model_catalog_tasks task
      WHERE task.provider_account_id = account.id
        AND (task.state IN ('PENDING', 'CLAIMED')
             OR (task.state = 'COMPLETED' AND task.account_version = account.version
                 AND task.provider_credential_revision_id = credential.id
                 AND task.completed_at > clock_timestamp() - interval '5 minutes')))
ORDER BY account.updated_at, account.id
LIMIT $1
FOR UPDATE OF account SKIP LOCKED;
