-- name: platform_membership__list :many
SELECT membership.ref,
       subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active,
       membership.role,
       membership.active,
       membership.version
FROM control_plane.memberships membership
JOIN control_plane.subjects subject
  ON subject.id = membership.subject_id
 AND subject.organization_id = membership.organization_id
WHERE membership.organization_id = @organization_id::uuid
  AND membership.project_id IS NULL
  AND subject.issuer = 'verified-oidc-subject'
ORDER BY subject.display_name, subject.ref
LIMIT @page_size;
