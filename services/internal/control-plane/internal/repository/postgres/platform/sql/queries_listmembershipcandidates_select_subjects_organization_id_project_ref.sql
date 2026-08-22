-- name: platform__queries_listmembershipcandidates_select_subjects_organization_id_project_ref :many
SELECT subject.ref,
       subject.display_name,
       subject.email_masked,
       subject.active
FROM control_plane.projects project
JOIN control_plane.subjects subject
  ON subject.organization_id = project.organization_id
WHERE project.organization_id = $1::uuid
  AND project.ref = $2
  AND project.lifecycle = 'ACTIVE'
  AND (
      $3 IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships actor_membership
          WHERE actor_membership.organization_id = project.organization_id
            AND actor_membership.project_id = project.id
            AND actor_membership.subject_id = $4::uuid
            AND actor_membership.active
            AND 'MANAGE_MEMBERS' = ANY(actor_membership.permissions)
      )
  )
  AND subject.active
  AND subject.issuer = 'verified-oidc-subject'
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.memberships platform_membership
      WHERE platform_membership.organization_id = project.organization_id
        AND platform_membership.subject_id = subject.id
        AND platform_membership.project_id IS NULL
        AND platform_membership.active
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.memberships project_membership
      WHERE project_membership.organization_id = project.organization_id
        AND project_membership.project_id = project.id
        AND project_membership.subject_id = subject.id
  )
  AND (
      $5 = ''
      OR subject.display_name ILIKE '%' || $5 || '%'
      OR subject.email_masked ILIKE '%' || $5 || '%'
  )
ORDER BY subject.display_name, subject.ref
LIMIT $6;
