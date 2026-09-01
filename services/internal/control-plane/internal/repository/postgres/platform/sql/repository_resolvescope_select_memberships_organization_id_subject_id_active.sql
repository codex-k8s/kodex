-- name: repository_resolvescope_select_memberships_organization_id_subject_id_active :one
SELECT organization.id::text,
       organization.ref,
       subject.id::text,
       subject.ref,
       subject.display_name,
       COALESCE((
         SELECT role.stable_key
         FROM control_plane.access_bindings binding
         JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
         JOIN control_plane.application_roles role ON role.id = role_version.role_id
         WHERE binding.organization_id = organization.id
           AND binding.subject_id = subject.id
           AND binding.state = 'ACTIVE'
           AND binding.scope_kind = 'ORGANIZATION'
           AND role.kind = 'SYSTEM'
           AND (binding.valid_from IS NULL OR binding.valid_from <= clock_timestamp())
           AND (binding.valid_until IS NULL OR binding.valid_until > clock_timestamp())
         ORDER BY CASE role.stable_key
           WHEN 'OWNER' THEN 1 WHEN 'ADMINISTRATOR' THEN 2 WHEN 'OPERATOR' THEN 3
           WHEN 'AUDITOR' THEN 4 ELSE 5 END
         LIMIT 1
       ), 'MEMBER')
FROM control_plane.organizations organization
JOIN control_plane.subjects subject
  ON subject.organization_id = organization.id
 AND subject.ref = $1
 AND subject.active
WHERE organization.ref = $2;
