-- name: platform_membership__update :one
WITH target_role AS (
    SELECT role_version.id, role.stable_key
    FROM control_plane.application_roles role
    JOIN control_plane.application_role_versions role_version ON role_version.id = role.current_version_id
    WHERE role.organization_id = @organization_id::uuid
      AND role.kind = 'SYSTEM' AND role.stable_key = @platform_role AND role.state = 'ACTIVE'
), updated AS (
    UPDATE control_plane.access_bindings binding
    SET role_version_id = target_role.id,
        state = CASE WHEN @active::boolean THEN 'ACTIVE' ELSE 'REVOKED' END,
        version = binding.version + 1,
        updated_at = clock_timestamp()
    FROM target_role
    WHERE binding.id = @membership_id::uuid
      AND binding.organization_id = @organization_id::uuid
      AND binding.presentation_kind = 'PLATFORM_MEMBERSHIP'
      AND binding.version = @expected_version
    RETURNING binding.ref, binding.role_version_id, binding.state, binding.version
)
SELECT updated.ref, target_role.stable_key, updated.state = 'ACTIVE', updated.version
FROM updated JOIN target_role ON target_role.id = updated.role_version_id;
