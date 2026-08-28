-- name: platform_membership__deactivate_projects :exec
UPDATE control_plane.access_bindings
SET state = 'REVOKED',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND subject_id = @subject_id::uuid
  AND presentation_kind = 'PROJECT_MEMBERSHIP'
  AND state = 'ACTIVE';
