-- name: model_catalog__lock_completion :one
SELECT task.id::text, task.state, task.request_digest, task.claimant_id, task.claim_generation, task.fence, task.expires_at,
       account.version = task.account_version AND account.current_credential_revision_id = task.provider_credential_revision_id
           AND account.enabled AND account.state = 'AUTHORIZED' AND definition.enabled,
       COALESCE(observation.request_digest, ''), COALESCE(observation.receipt_digest, ''), clock_timestamp(),
       account.ref, task.account_version, account.definition_key, credential.ref, credential.revision_number,
       credential.secret_name, credential.secret_uid::text, credential.secret_resource_version, credential.content_sha256,
       task.authorization_method
FROM control_plane.provider_model_catalog_tasks task
JOIN control_plane.provider_accounts account ON account.id = task.provider_account_id AND account.organization_id = task.organization_id
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
JOIN control_plane.provider_credential_revisions credential ON credential.id = task.provider_credential_revision_id
  AND credential.provider_account_id = account.id AND credential.organization_id = task.organization_id
LEFT JOIN control_plane.provider_model_catalog_observations observation ON observation.task_id = task.id
WHERE task.organization_id = $1::uuid AND task.ref = $2
FOR UPDATE OF account, task;
