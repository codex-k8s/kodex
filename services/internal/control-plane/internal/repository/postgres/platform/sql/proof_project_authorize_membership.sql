-- name: platform__proof_project_authorize_membership :one
SELECT p.id::text, p.version
FROM control_plane.projects p
WHERE p.ref = $1 AND p.organization_id = $2::uuid AND p.lifecycle = 'ACTIVE'
  AND EXISTS (
      SELECT 1
      FROM control_plane.memberships platform_membership
      WHERE platform_membership.organization_id = p.organization_id
        AND platform_membership.subject_id = $3::uuid
        AND platform_membership.project_id IS NULL
        AND platform_membership.active
        AND (
            platform_membership.role IN ('OWNER', 'ADMINISTRATOR')
            OR EXISTS (
                SELECT 1
                FROM control_plane.memberships project_membership
                WHERE project_membership.organization_id = p.organization_id
                  AND project_membership.project_id = p.id
                  AND project_membership.subject_id = platform_membership.subject_id
                  AND project_membership.active
            )
        )
  )
