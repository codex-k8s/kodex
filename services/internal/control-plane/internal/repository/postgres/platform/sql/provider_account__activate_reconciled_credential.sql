-- name: provider_account__activate_reconciled_credential :exec
UPDATE control_plane.provider_accounts
SET current_credential_revision_id = @next_credential_revision_id::uuid,
    state = 'AUTHORIZED',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @provider_account_id::uuid
  AND current_credential_revision_id = @current_credential_revision_id::uuid;
