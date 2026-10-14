-- name: provider_renewal__runtime_candidate :one
SELECT account.current_credential_revision_id = $3::uuid
    AND NOT EXISTS (SELECT 1 FROM control_plane.provider_credential_renewal_tasks task
        WHERE task.organization_id = account.organization_id AND task.provider_account_id = account.id
            AND task.input_credential_id = account.current_credential_revision_id
            AND task.state IN ('CLAIMED', 'AUTHORIZED', 'UNKNOWN_OUTCOME'))
FROM control_plane.provider_accounts account
WHERE account.organization_id = $1::uuid AND account.id = $2::uuid;
