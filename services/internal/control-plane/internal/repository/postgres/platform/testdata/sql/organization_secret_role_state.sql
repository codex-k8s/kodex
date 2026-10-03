-- name: organization_secret_role_state_fixture :exec
UPDATE control_plane.access_bindings binding
SET state = $2
FROM control_plane.subjects subject,
     control_plane.application_role_versions role_version,
     control_plane.application_roles role
WHERE subject.id::text = $1 AND binding.subject_id = subject.id
  AND binding.organization_id = subject.organization_id
  AND binding.scope_kind = 'ORGANIZATION'
  AND role_version.id = binding.role_version_id
  AND role.id = role_version.role_id AND role.kind = 'SYSTEM'
  AND role.stable_key = 'OWNER';
