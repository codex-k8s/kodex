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
  AND project.ref = @project_ref
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
ORDER BY subject.display_name, subject.ref
LIMIT @page_size;
