-- name: project_membership__deactivate :one
UPDATE control_plane.access_bindings
SET state = 'REVOKED',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @membership_id::uuid
  AND organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND presentation_kind = 'PROJECT_MEMBERSHIP'
  AND version = @expected_version
  AND state = 'ACTIVE'
RETURNING ref,
          control_plane.legacy_permissions((
              SELECT role_version.permission_keys
              FROM control_plane.application_role_versions role_version
              WHERE role_version.id = access_bindings.role_version_id
          )),
          state = 'ACTIVE',
          version;
