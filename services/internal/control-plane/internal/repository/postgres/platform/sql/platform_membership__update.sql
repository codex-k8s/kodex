-- name: platform_membership__update :one
UPDATE control_plane.memberships
SET role = @platform_role,
    active = @active,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @membership_id::uuid
  AND organization_id = @organization_id::uuid
  AND project_id IS NULL
  AND version = @expected_version
RETURNING ref, role, active, version;
