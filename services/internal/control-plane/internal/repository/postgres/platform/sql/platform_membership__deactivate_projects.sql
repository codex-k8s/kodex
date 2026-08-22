-- name: platform_membership__deactivate_projects :exec
UPDATE control_plane.memberships
SET active = false,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND subject_id = @subject_id::uuid
  AND project_id IS NOT NULL
  AND active;
