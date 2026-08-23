-- name: platform_membership__list_candidates :many
SELECT subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active
FROM control_plane.subjects subject
WHERE subject.organization_id = @organization_id::uuid
  AND subject.active
  AND subject.issuer = 'verified-oidc-subject'
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.memberships membership
      WHERE membership.organization_id = subject.organization_id
        AND membership.subject_id = subject.id
        AND membership.project_id IS NULL
  )
  AND (
      @query = ''
      OR subject.display_name ILIKE '%' || @query || '%'
      OR subject.email_masked ILIKE '%' || @query || '%'
  )
ORDER BY subject.display_name, subject.ref
LIMIT @page_size;
