-- name: provider_renewal__input_eligible :one
SELECT account.state = 'AUTHORIZED' AND account.enabled AND definition.enabled
       AND account.version = $3 AND account.current_credential_revision_id = $4::uuid
       AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
           JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id AND revision.organization_id = lease.organization_id
           WHERE lease.organization_id = account.organization_id AND revision.provider_account_id = account.id AND lease.state = 'CLAIMED')
FROM control_plane.provider_accounts account
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE account.organization_id = $1::uuid AND account.id = $2::uuid;
