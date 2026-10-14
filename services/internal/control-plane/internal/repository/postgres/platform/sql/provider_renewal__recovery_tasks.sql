-- name: provider_renewal__recovery_tasks :many
SELECT task.ref, task.organization_id::text, account.ref, task.account_version, credential.ref, credential.revision_number,
       credential.secret_name, credential.secret_uid::text, credential.secret_resource_version, credential.content_sha256,
       task.claimant_id, task.generation, task.fence, task.request_digest, task.owner_request_digest, task.expires_at, task.state
FROM control_plane.provider_credential_renewal_tasks task
JOIN control_plane.provider_accounts account ON account.id = task.provider_account_id AND account.organization_id = task.organization_id
JOIN control_plane.provider_credential_revisions credential ON credential.id = task.input_credential_id
    AND credential.provider_account_id = account.id AND credential.organization_id = task.organization_id
WHERE task.organization_id = $1::uuid AND task.state = 'UNKNOWN_OUTCOME' AND account.enabled AND account.state = 'AUTHORIZED'
    AND account.version = task.account_version AND account.current_credential_revision_id = task.input_credential_id
ORDER BY task.completed_at, task.id LIMIT 1;
