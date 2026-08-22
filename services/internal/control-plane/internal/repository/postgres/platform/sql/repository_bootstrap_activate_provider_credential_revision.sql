-- name: platform__repository_bootstrap_activate_provider_credential_revision :exec
UPDATE control_plane.provider_accounts
SET current_credential_revision_id = $2::uuid,
    state = 'AUTHORIZED',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1::uuid
  AND current_credential_revision_id IS NULL;
