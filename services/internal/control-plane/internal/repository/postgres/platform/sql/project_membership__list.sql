-- name: project_membership__list :many
SELECT project_membership.ref,
       project.ref,
       subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active,
       platform_membership.role,
       project_membership.permissions,
       project_membership.active,
       project_membership.version
FROM control_plane.projects project
JOIN control_plane.memberships project_membership
  ON project_membership.project_id = project.id
 AND project_membership.organization_id = project.organization_id
JOIN control_plane.subjects subject
  ON subject.id = project_membership.subject_id
 AND subject.organization_id = project.organization_id
JOIN control_plane.memberships platform_membership
  ON platform_membership.organization_id = project.organization_id
 AND platform_membership.subject_id = subject.id
 AND platform_membership.project_id IS NULL
WHERE project.organization_id = @organization_id::uuid
  AND (@project_ref = '' OR project.ref = @project_ref)
  AND (@query = '' OR subject.display_name ILIKE '%' || @query || '%' OR subject.email_masked ILIKE '%' || @query || '%')
  AND (@cursor_ref = '' OR project_membership.ref > @cursor_ref)
ORDER BY project_membership.ref
LIMIT @page_size;
