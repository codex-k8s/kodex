-- name: ssh_execution_test__isolate_acl :many
UPDATE control_plane.access_bindings binding SET state='REVOKED',version=binding.version+1
FROM control_plane.application_role_versions version
WHERE binding.organization_id=$1::uuid AND binding.role_version_id=version.id
 AND binding.state='ACTIVE' AND binding.ref<>$3 AND $4=ANY(version.permission_keys)
 AND(binding.subject_id=$2::uuid OR binding.oidc_group_id IN(
  SELECT membership.group_id FROM control_plane.oidc_group_memberships membership
  WHERE membership.organization_id=$1::uuid AND membership.subject_id=$2::uuid))
RETURNING binding.id::text;
