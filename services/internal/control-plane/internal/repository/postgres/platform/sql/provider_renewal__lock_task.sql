-- name: provider_renewal__lock_task :one
SELECT task.id::text, task.state, task.organization_id::text, account.id::text,
       task.ref, account.ref, task.account_version, credential.id::text, credential.ref, credential.revision_number,
       credential.secret_name, credential.secret_uid::text, credential.secret_resource_version, credential.content_sha256,
       task.claimant_id, task.generation, task.fence, task.request_digest, task.owner_request_digest, task.expires_at,
       account.version = task.account_version AND account.current_credential_revision_id = task.input_credential_id
           AND account.state = 'AUTHORIZED' AND account.enabled AND definition.enabled,
       clock_timestamp(), COALESCE(task.result_digest, ''), COALESCE(task.output_credential_id::text, '')
FROM control_plane.provider_credential_renewal_tasks task
JOIN control_plane.provider_accounts account ON account.id = task.provider_account_id AND account.organization_id = task.organization_id
JOIN control_plane.provider_credential_revisions credential ON credential.id = task.input_credential_id
    AND credential.organization_id = task.organization_id AND credential.provider_account_id = account.id
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE task.organization_id = $1::uuid AND task.ref = $2
FOR UPDATE OF account, task;
