-- name: access_activate_role_version :one
UPDATE control_plane.application_roles
SET current_version_id = @role_version_id::uuid,
    version = CASE WHEN current_version_id IS NULL THEN version ELSE version + 1 END,
    updated_at = clock_timestamp()
WHERE id = @role_id::uuid
  AND organization_id = @organization_id::uuid
  AND (@expected_version::bigint = 0 OR version = @expected_version::bigint)
RETURNING version, updated_at
