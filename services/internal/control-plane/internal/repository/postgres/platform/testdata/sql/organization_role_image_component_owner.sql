-- name: organization_role_image_component_owner :exec
UPDATE control_plane.access_bindings binding SET role_version_id=role.current_version_id
FROM control_plane.application_roles role
WHERE binding.organization_id=$1::uuid AND binding.subject_id=$2::uuid
  AND binding.project_id IS NULL AND binding.presentation_kind='PLATFORM_MEMBERSHIP'
  AND role.organization_id=binding.organization_id AND role.kind='SYSTEM' AND role.stable_key=$3;
