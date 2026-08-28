-- name: access_archive_role :one
UPDATE control_plane.application_roles
SET state = 'ARCHIVED', version = version + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND ref = @role_ref
  AND kind = 'CUSTOM' AND state = 'ACTIVE' AND version = @expected_version
RETURNING version, updated_at
