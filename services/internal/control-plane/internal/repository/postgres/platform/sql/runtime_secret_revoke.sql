-- name: runtime_secret_revoke :one
UPDATE control_plane.runtime_secrets
SET state = 'REVOKED', version = version + 1,
    display_hint_prefix = '', display_hint_suffix = '', updated_at = clock_timestamp()
WHERE id = @secret_id::uuid AND state = 'ACTIVE'
  AND version = @expected_version
  AND current_revision = @expected_current_revision
RETURNING version, state, current_revision, updated_at;
