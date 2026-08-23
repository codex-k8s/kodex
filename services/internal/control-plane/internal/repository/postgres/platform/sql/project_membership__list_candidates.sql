-- name: project_membership__list_candidates :many
SELECT subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active
FROM control_plane.projects project
JOIN control_plane.memberships platform_membership
  ON platform_membership.organization_id = project.organization_id
 AND platform_membership.project_id IS NULL
 AND platform_membership.active
JOIN control_plane.subjects subject
  ON subject.id = platform_membership.subject_id
 AND subject.organization_id = project.organization_id
WHERE project.organization_id = @organization_id::uuid
  AND project.ref = @project_ref
  AND project.lifecycle = 'ACTIVE'
  AND (
      @actor_platform_role IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships actor_membership
          WHERE actor_membership.organization_id = project.organization_id
            AND actor_membership.project_id = project.id
            AND actor_membership.subject_id = @actor_id::uuid
            AND actor_membership.active
            AND 'MANAGE_MEMBERS' = ANY(actor_membership.permissions)
      )
  )
  AND subject.active
  AND subject.issuer = 'verified-oidc-subject'
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.memberships project_membership
      WHERE project_membership.organization_id = project.organization_id
        AND project_membership.project_id = project.id
        AND project_membership.subject_id = subject.id
  )
  AND (
      @query = ''
      OR subject.display_name ILIKE '%' || @query || '%'
      OR subject.email_masked ILIKE '%' || @query || '%'
  )
ORDER BY subject.display_name, subject.ref
LIMIT @page_size;
