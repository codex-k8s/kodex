-- name: project_membership__deactivate :one
UPDATE control_plane.memberships
SET active = false,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @membership_id::uuid
  AND organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND version = @expected_version
  AND active
RETURNING ref, permissions, active, version;
