-- name: platform_membership__resolve_for_update :one
SELECT membership.id::text,
       membership.subject_id::text,
       membership.ref,
       subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active,
       membership.role,
       membership.active,
       membership.version
FROM control_plane.organizations organization
JOIN control_plane.memberships membership
  ON membership.organization_id = organization.id
 AND membership.project_id IS NULL
JOIN control_plane.subjects subject
  ON subject.id = membership.subject_id
 AND subject.organization_id = organization.id
WHERE organization.id = @organization_id::uuid
  AND membership.ref = @membership_ref
  AND subject.issuer = 'verified-oidc-subject'
FOR UPDATE OF organization, membership;
