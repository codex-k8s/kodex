-- name: provider_accounts_update_lifecycle :exec
UPDATE control_plane.provider_accounts
SET state = @state,
    enabled = @enabled,
    current_credential_revision_id = CASE WHEN @clear_credential THEN NULL ELSE current_credential_revision_id END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @account_id::uuid;
