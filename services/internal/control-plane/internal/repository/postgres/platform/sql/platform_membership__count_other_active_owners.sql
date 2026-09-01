-- name: platform_membership__count_other_active_owners :one
SELECT count(*)
FROM control_plane.access_bindings binding
JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
JOIN control_plane.application_roles role ON role.id = role_version.role_id
WHERE binding.organization_id = @organization_id::uuid
  AND binding.presentation_kind = 'PLATFORM_MEMBERSHIP'
  AND role.kind = 'SYSTEM' AND role.stable_key = 'OWNER'
  AND binding.state = 'ACTIVE'
  AND (binding.valid_from IS NULL OR binding.valid_from <= clock_timestamp())
  AND (binding.valid_until IS NULL OR binding.valid_until > clock_timestamp())
  AND binding.id <> @membership_id::uuid;
