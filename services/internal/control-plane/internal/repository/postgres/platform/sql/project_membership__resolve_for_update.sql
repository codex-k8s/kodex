-- name: project_membership__resolve_for_update :one
SELECT binding.id::text,
       binding.subject_id::text,
       binding.ref,
       subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active,
       platform_membership.role,
       control_plane.legacy_permissions(role_version.permission_keys),
       binding.state = 'ACTIVE',
       binding.version
FROM control_plane.projects project
JOIN control_plane.access_bindings binding
  ON binding.organization_id = project.organization_id
 AND binding.project_id = project.id
 AND binding.presentation_kind = 'PROJECT_MEMBERSHIP'
JOIN control_plane.subjects subject
  ON subject.id = binding.subject_id
 AND subject.organization_id = project.organization_id
JOIN control_plane.memberships platform_membership
  ON platform_membership.organization_id = project.organization_id
 AND platform_membership.subject_id = subject.id
 AND platform_membership.project_id IS NULL
JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
WHERE project.organization_id = @organization_id::uuid
  AND project.id = @project_id::uuid
  AND binding.ref = @membership_ref
FOR UPDATE OF project, binding;
