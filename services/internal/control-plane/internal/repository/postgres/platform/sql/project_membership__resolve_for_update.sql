-- name: project_membership__resolve_for_update :one
SELECT project_membership.id::text,
       project_membership.subject_id::text,
       project_membership.ref,
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
  ON project_membership.organization_id = project.organization_id
 AND project_membership.project_id = project.id
JOIN control_plane.subjects subject
  ON subject.id = project_membership.subject_id
 AND subject.organization_id = project.organization_id
JOIN control_plane.memberships platform_membership
  ON platform_membership.organization_id = project.organization_id
 AND platform_membership.subject_id = subject.id
 AND platform_membership.project_id IS NULL
WHERE project.organization_id = @organization_id::uuid
  AND project.id = @project_id::uuid
  AND project_membership.ref = @membership_ref
FOR UPDATE OF project, project_membership;
