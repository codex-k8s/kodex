-- name: platform_membership__resolve_for_update :one
SELECT binding.id::text,
       binding.subject_id::text,
       binding.ref,
       subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active,
       role.stable_key,
       binding.state = 'ACTIVE',
       binding.version
FROM control_plane.organizations organization
JOIN control_plane.access_bindings binding
  ON binding.organization_id = organization.id
 AND binding.presentation_kind = 'PLATFORM_MEMBERSHIP'
JOIN control_plane.subjects subject
  ON subject.id = binding.subject_id
 AND subject.organization_id = organization.id
JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
JOIN control_plane.application_roles role ON role.id = role_version.role_id AND role.kind = 'SYSTEM'
WHERE organization.id = @organization_id::uuid
  AND binding.ref = @membership_ref
  AND subject.issuer = 'verified-oidc-subject'
FOR UPDATE OF organization, binding;
