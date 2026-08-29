-- name: runtime_secret_activate :one
UPDATE control_plane.runtime_secrets
SET state = 'ACTIVE', version = version + 1, current_revision = @revision,
    display_hint_prefix = @hint_prefix, display_hint_suffix = @hint_suffix,
    updated_at = clock_timestamp()
WHERE id = @secret_id::uuid
  AND version = @expected_version
  AND current_revision = @expected_current_revision
  AND state = @expected_state
RETURNING version, state, current_revision, display_hint_prefix, display_hint_suffix, updated_at;
