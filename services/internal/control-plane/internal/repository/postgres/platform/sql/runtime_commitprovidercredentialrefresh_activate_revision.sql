-- name: runtime_commitprovidercredentialrefresh_activate_revision :exec
UPDATE control_plane.provider_accounts
SET current_credential_revision_id = $3::uuid,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1::uuid
  AND current_credential_revision_id = $2::uuid
  AND ((state = 'AUTHORIZED' AND enabled) OR state = 'DELETING');
