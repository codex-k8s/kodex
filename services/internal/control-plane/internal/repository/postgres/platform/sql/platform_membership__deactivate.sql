-- name: platform_membership__deactivate :one
WITH updated AS (
    UPDATE control_plane.access_bindings binding
    SET state = 'REVOKED',
        version = binding.version + 1,
        updated_at = clock_timestamp()
    WHERE binding.id = @membership_id::uuid
      AND binding.organization_id = @organization_id::uuid
      AND binding.presentation_kind = 'PLATFORM_MEMBERSHIP'
      AND binding.version = @expected_version
      AND binding.state = 'ACTIVE'
    RETURNING binding.ref, binding.role_version_id, binding.state, binding.version
)
SELECT updated.ref, role.stable_key, updated.state = 'ACTIVE', updated.version
FROM updated
JOIN control_plane.application_role_versions role_version ON role_version.id = updated.role_version_id
JOIN control_plane.application_roles role ON role.id = role_version.role_id AND role.kind = 'SYSTEM';
