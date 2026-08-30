-- name: provider_accounts_activate_credential :exec
UPDATE control_plane.provider_accounts
SET current_credential_revision_id = @credential_id::uuid,
    external_account_masked = @external_account_masked,
    state = 'AUTHORIZED',
    enabled = true,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @account_id::uuid;
