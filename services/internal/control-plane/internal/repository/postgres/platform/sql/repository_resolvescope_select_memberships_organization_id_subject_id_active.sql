-- name: repository_resolvescope_select_memberships_organization_id_subject_id_active :one
SELECT organization.id::text,
       organization.ref,
       subject.id::text,
       subject.ref,
       subject.display_name,
       platform_membership.role
FROM control_plane.organizations organization
JOIN control_plane.subjects subject
  ON subject.organization_id = organization.id
 AND subject.ref = $1
 AND subject.active
JOIN control_plane.memberships platform_membership
  ON platform_membership.organization_id = organization.id
 AND platform_membership.subject_id = subject.id
 AND platform_membership.project_id IS NULL
 AND platform_membership.active
WHERE organization.ref = $2;
